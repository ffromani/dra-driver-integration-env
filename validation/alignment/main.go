// AI-Attribution: AIA HAb Nc Hin R gemini-3.0-pro v1.0

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/stdr"
	"github.com/moby/moby/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/cpuset"
)

const (
	cgroupRoot = "/sys/fs/cgroup"
	nodeRoot   = "/sys/devices/system/node"
)

// Topology maps NUMA ID -> CPUSet
type Topology map[int]cpuset.CPUSet

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run check-alignment.go <namespace> <pod-name>")
		os.Exit(1)
	}

	lh := stdr.New(log.New(os.Stderr, "", log.Lshortfile))
	podNamespace, podName := os.Args[1], os.Args[2]

	topo, err := loadPhysicalTopology()
	if err != nil {
		lh.Error(err, "getting the NUMA CPU topology")
		os.Exit(1)
	}
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		lh.Error(err, "loading a kubernetes client config")
		os.Exit(1)
	}
	kubeCli, err := kubernetes.NewForConfig(config)
	if err != nil {
		lh.Error(err, "creating a kubernetes client")
		os.Exit(1)
	}
	mobyCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		lh.Error(err, "creating a moby client")
		os.Exit(1)
	}

	ctx := context.Background()

	pod, err := kubeCli.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		lh.Error(err, "getting the pod %s/%s", podNamespace, podName)
		os.Exit(1)
	}

	// Kind names the container exactly as the K8s node name
	nodeContainer, err := mobyCli.ContainerInspect(ctx, pod.Spec.NodeName, client.ContainerInspectOptions{})
	if err != nil {
		lh.Error(err, "inspecting Kind node container %q", pod.Spec.NodeName)
		os.Exit(1)
	}
	kindNodeID := nodeContainer.Container.ID

	header := fmt.Sprintf("Pod: %s/%s QoS=%s running on node: %s (Docker ID: %.12s)",
		pod.Namespace, pod.Name, pod.Status.QOSClass, pod.Spec.NodeName, kindNodeID)
	fmt.Println(header)
	fmt.Println(strings.Repeat("=", len(header)))

	for _, status := range pod.Status.ContainerStatuses {
		fmt.Printf("+ Container: %s\n", status.Name)

		cgroupPath, err := findKindCgroupPath(kindNodeID, pod, status.ContainerID)
		if err != nil {
			lh.Error(err, "getting the cgroup path")
			continue
		}

		assignedCPUs, err := loadCPUSetFromFile(filepath.Join(cgroupPath, "cpuset.cpus"))
		if err != nil {
			lh.Error(err, "getting the container CPUs")
			continue
		}
		assignedNodes, err := loadCPUSetFromFile(filepath.Join(cgroupPath, "cpuset.mems"))
		if err != nil {
			lh.Error(err, "getting the container NUMA Nodes")
			continue
		}

		fmt.Println("  + Memory:")
		for _, nodeID := range assignedNodes.List() {
			fmt.Printf("    + NUMA Zone %d\n", nodeID)
		}
		fmt.Println("  + CPU:")
		for nodeID, nodeCPUs := range topo {
			matches := nodeCPUs.Difference(assignedCPUs)
			if !matches.IsEmpty() {
				fmt.Printf("    + NUMA Zone %d: CPUs %v\n", nodeID, matches)
			}
		}
		fmt.Println()
	}
}

func findKindCgroupPath(kindNodeID string, pod *corev1.Pod, fullContainerID string) (string, error) {
	parts := strings.Split(fullContainerID, "://")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid container ID")
	}
	runtime, containerID := parts[0], parts[1]

	podUID := strings.ReplaceAll(string(pod.UID), "-", "_")

	// Base path for Kind Node on host
	// Note: On some systems it's /sys/fs/cgroup/system.slice/docker-<ID>.scope
	// On others (cgroupv2) it might be /sys/fs/cgroup/machine.slice/docker-<ID>.scope
	baseKindPath := filepath.Join(cgroupRoot, "system.slice", fmt.Sprintf("docker-%s.scope", kindNodeID))

	// Kind's internal K8s hierarchy
	var qosSlice string
	switch pod.Status.QOSClass {
	case corev1.PodQOSBurstable:
		qosSlice = "kubelet-kubepods-burstable.slice"
	case corev1.PodQOSBestEffort:
		qosSlice = "kubelet-kubepods-besteffort.slice"
	default:
		qosSlice = ""
	}

	var containerScope string
	if runtime == "containerd" {
		containerScope = fmt.Sprintf("cri-containerd-%s.scope", containerID)
	} else {
		containerScope = fmt.Sprintf("crio-%s.scope", containerID)
	}

	path := filepath.Clean(filepath.Join(baseKindPath, "kubelet.slice", "kubelet-kubepods.slice", qosSlice))

	var qosSubSlice string
	switch pod.Status.QOSClass {
	case corev1.PodQOSBurstable:
		qosSubSlice = fmt.Sprintf("kubelet-kubepods-burstable-pod%s.slice", podUID)
	case corev1.PodQOSBestEffort:
		qosSubSlice = fmt.Sprintf("kubelet-kubepods-besteffort-pod%s.slice", podUID)
	default:
		qosSubSlice = fmt.Sprintf("kubelet-kubepods-pod%s.slice", podUID)
	}
	path = filepath.Join(path, qosSubSlice, containerScope)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("path not found: %s", path)
	}
	return path, nil
}

func loadPhysicalTopology() (Topology, error) {
	topo := make(Topology)
	nodes, _ := filepath.Glob(filepath.Join(nodeRoot, "node[0-9]*"))
	for _, nodePath := range nodes {
		nodeIDStr := filepath.Base(nodePath)[4:]
		nodeID, _ := strconv.Atoi(nodeIDStr)
		cpus, err := loadCPUSetFromFile(filepath.Join(nodePath, "cpulist"))
		if err != nil {
			return nil, err
		}
		topo[nodeID] = cpus
	}
	return topo, nil
}

func loadCPUSetFromFile(fpath string) (cpuset.CPUSet, error) {
	cpuListRaw, err := os.ReadFile(fpath)
	if err != nil {
		return cpuset.CPUSet{}, err
	}
	return cpuset.Parse(strings.TrimSpace(string(cpuListRaw)))
}
