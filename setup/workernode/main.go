/*
 * Copyright 2026 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/yaml"
)

const (
	// roleWorker contains the worker role
	roleWorker = "worker"
)

const (
	// labelRole contains the key for the role label
	labelRole = "node-role.kubernetes.io"
	// labelHostname contains the key for the hostname label
	labelHostname = "kubernetes.io/hostname"
)

// getWorkerNodes returns all nodes labeled as worker
func getWorkerNodes(ctx context.Context, cs kubernetes.Interface) ([]corev1.Node, error) {
	return getNodesByRole(ctx, cs, roleWorker)
}

// getByRole returns all nodes with the specified role
func getNodesByRole(ctx context.Context, cs kubernetes.Interface, role string) ([]corev1.Node, error) {
	selector, err := labels.Parse(fmt.Sprintf("%s/%s=", labelRole, role))
	if err != nil {
		return nil, err
	}
	return getNodesBySelector(ctx, cs, selector)
}

// getBySelector returns all nodes with the specified selector
func getNodesBySelector(ctx context.Context, cs kubernetes.Interface, selector labels.Selector) ([]corev1.Node, error) {
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	return nodes.Items, nil
}

type chrootJob struct {
	Name           string
	Namespace      string
	ServiceAccount string
	Image          string
	Kubeconfig     string
}

func chrootJobWithDefaults() chrootJob {
	return chrootJob{
		Name:           "drasetup",
		Namespace:      "kube-system",
		ServiceAccount: "drasetup",
	}
}

func (cj chrootJob) ValidateOrFail() {
	if cj.Namespace == "" {
		log.Fatalf("missing job namespace")
	}
	if cj.Name == "" {
		log.Fatalf("missing job name")
	}
	if cj.ServiceAccount == "" {
		log.Fatalf("missing service account")
	}
	if cj.Image == "" {
		log.Fatalf("missing image")
	}
}

func main() {
	cj := chrootJobWithDefaults()
	flag.StringVar(&cj.Name, "name", cj.Name, "task name")
	flag.StringVar(&cj.Namespace, "namespace", cj.Namespace, "task namespace")
	flag.StringVar(&cj.Image, "image", cj.Image, "container image name")
	flag.StringVar(&cj.ServiceAccount, "serviceaccount", cj.ServiceAccount, "serviceAccount to run jobs under")
	flag.StringVar(&cj.Kubeconfig, "kubeconfig", cj.Kubeconfig, "absolute path to the kubeconfig file")
	flag.Parse()

	cj.ValidateOrFail()

	cs, err := createClientset(cj.Kubeconfig)
	if err != nil {
		log.Fatalf("creating the client: %v", err)
	}

	ctx := context.Background()
	workers, err := getWorkerNodes(ctx, cs)
	if err != nil {
		log.Fatalf("getting worker nodes: %v", err)
	}

	job, err := makeSetupRuntimeJob(cj, workers)
	if err != nil {
		log.Fatalf("preparing the job definitions: %v", err)
	}
	jobData, err := yaml.Marshal(job)
	if err != nil {
		log.Fatalf("creating the job YAML: %v", err)
	}
	fmt.Println(string(jobData))
}

func createClientset(kubeconfig string) (kubernetes.Interface, error) {
	var err error
	var config *rest.Config
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		// creates the in-cluster config
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	// use protobuf for better performance at scale
	// https://kubernetes.io/docs/reference/using-api/api-concepts/#alternate-representations-of-resources
	config.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	config.ContentType = "application/vnd.kubernetes.protobuf"
	return kubernetes.NewForConfig(config)
}

func makeSetupRuntimeJob(cj chrootJob, workers []corev1.Node) (*batchv1.Job, error) {
	parallelism := int32(len(workers))

	hostPathDirectory := corev1.HostPathDirectory
	root_ := int64(0)
	true_ := true
	jobLabels := map[string]string{
		"app": cj.Name,
	}
	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    cj.Namespace,
			GenerateName: cj.Name + "-",
		},
		Spec: batchv1.JobSpec{
			Parallelism: &parallelism,
			Completions: &parallelism,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: jobLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "setup-runtime",
							Image: cj.Image,
							Command: []string{
								"/bin/setup-runtime",
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &true_,
								RunAsUser:  &root_,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc",
									Name:      "etc",
								},
							},
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: cj.ServiceAccount,
					NodeSelector: map[string]string{
						fmt.Sprintf("%s/%s", labelRole, roleWorker): "",
					},
					HostNetwork: true,
					HostPID:     true,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser: &root_,
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							WhenUnsatisfiable: corev1.DoNotSchedule,
							TopologyKey:       "kubernetes.io/hostname",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: jobLabels,
							},
							MatchLabelKeys: []string{
								"pod-template-hash",
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "etc",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc",
									Type: &hostPathDirectory,
								},
							},
						},
					},
				},
			},
		},
	}
	return job, nil
}
