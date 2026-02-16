DRA driver integration environment
==================================

A testbench and local lab to try integration of multiple DRA drivers.
This lab focuses on the Network Function flows, and include example workloads
which exercise DRA-backed CPU, Memory and SRIOV network device allocation.

# Requirements

* golang >= 1.25
* kubectl (or equivalent) to interact with the kubernetes cluster
* [kind](https://kind.sigs.k8s.io/) >= 0.31
* [minikube](https://minikube.sigs.k8s.io/docs/) >= 1.38

# Quickstart

## kind-based environments

First, you can use `make help` to get a list of the supported targets.
A fair amount of targets is used internally, the following quickstart
will illustrate the more useful for humans.

Set up the kind cluster:
```bash
make kind-setup
```

Once the step completes successfully, the kind cluster would look like this
```bash
$ kubectl get pods -A
NAMESPACE            NAME                                                              READY   STATUS    RESTARTS   AGE
kube-system          coredns-7d764666f9-f2n2w                                          1/1     Running   0          48s
kube-system          coredns-7d764666f9-rbzts                                          1/1     Running   0          48s
kube-system          dracpu-ksl5t                                                      1/1     Running   0          28s
kube-system          dracpu-xxtdg                                                      1/1     Running   0          28s
kube-system          dramemory-7fncq                                                   1/1     Running   0          28s
kube-system          dramemory-pz6z9                                                   1/1     Running   0          28s
kube-system          etcd-dra-core-resource-drivers-control-plane                      1/1     Running   0          57s
kube-system          kindnet-2l5qf                                                     1/1     Running   0          44s
kube-system          kindnet-5splj                                                     1/1     Running   0          48s
kube-system          kube-apiserver-dra-core-resource-drivers-control-plane            1/1     Running   0          57s
kube-system          kube-controller-manager-dra-core-resource-drivers-control-plane   1/1     Running   0          56s
kube-system          kube-proxy-875g5                                                  1/1     Running   0          48s
kube-system          kube-proxy-v74gp                                                  1/1     Running   0          44s
kube-system          kube-scheduler-dra-core-resource-drivers-control-plane            1/1     Running   0          57s
local-path-storage   local-path-provisioner-67b8995b4b-h9xcq                           1/1     Running   0          48s
```

The environment is now ready to test the DRA-based resource allocation.
There are example workloads in `workloads/...`. Every workload scenario has a `README.md` file which summarize the scenario.

Let's run the simplest scenario.
First we can create a `ResourceClaimTemplate` each pod can draw from.
```bash
$ kubectl create -f workloads/minimal-multi/resourceclaimtemplate.yaml
resourceclaimtemplate.resource.k8s.io/lowlat-minimal created
```

now we can create any number of pods using the `ResourceClaimTemplate` to allocate resources. The only real limitation
here are the compute resources of the machine running the lab.
```bash
$ for x in `seq 1 4`; do kubectl create -f workloads/minimal-multi/pod.yaml; done
pod/pod-lowlat-minimal-sk2kd created
pod/pod-lowlat-minimal-tclh5 created
pod/pod-lowlat-minimal-zspnd created
pod/pod-lowlat-minimal-p8lh6 created
```

if you want or need, there are some basic reference workloads which do *NOT* allocate exclusive resources
```bash
$ kubectl create -f workloads/reference/pod-burstable.yaml
pod/reference-burstable created
```

now the cluster should look like this
```bash
$ kubectl get pods
NAME                       READY   STATUS    RESTARTS   AGE
pod-lowlat-minimal-p8lh6   1/1     Running   0          72s
pod-lowlat-minimal-sk2kd   1/1     Running   0          72s
pod-lowlat-minimal-tclh5   1/1     Running   0          72s
pod-lowlat-minimal-zspnd   1/1     Running   0          72s
reference-burstable        1/1     Running   0          28s
```

The lab provides basic validation tooling. We can build them with
```bash
make build-validate-all
```

Some validation tools provided by the project exploit the fact the kind clusters run on the same host.
This fact allows to directly inspect the cgroup layout and other noteworthy system settings.
Therefore, the kind-specific validation cannot be used in other cluster flavours, like minikube.

The kind-specific validation tools (and in general, kind-specific bits) all have the `kind-` prefix.
The project also provides generic and flavor-agnostic validation tools, which can be used on both
kind and minikube-based clusters.

To remove the kind cluster:
```bash
make kind-teardown
```

## minikube-based environments

you can run test environment backed by minikube, in turn using the kvm2 driver and VM for kubernetes nodes.
The user experience largely overlaps. You just need to use targets starting with `minikube-` rather than starting
with `kind-`. Example

Set up the minikube cluster:
```bash
make minikube-setup
```

Remove the minikube cluster:
```bash
make minikube-teardown
```

### SRIOV emulation

Being VM based enable the minikube-based environment to support device emulation.
We provide the tools to augment the minikube VMs so the nodes have a `igb` device which enables SRIOV emulation.
This is helpful to integrate the [DRA SRIOV driver](https://github.com/k8snetworkplumbingwg/dra-driver-sriov/).

To enable SRIOV emulation, we need to run extra steps.
First, we need to inject the SRIOV-capable intel IGB devices in the minikube worker nodes. To do that, the project ships
the `virtinjected` tool and a libvirt hook. Make sure to build the `virtinjectd` tool using
```bash
build-virtinjectd
```

Then,  we need changes in the ISO image minikube uses to run the nodes.
A summary of changes, and pre-built ISO images, are available in the [support repo](https://github.com/ffromani/dra-driver-integration-env-data/blob/main/minikube-sriov/README.md).

You can download the pre-built ISO images with the SRIOV changes, or rebuild your own using
the patches and the instructions provided in the support repo.
Once you have your custom image, consume it using
```bash
$ export MINIKUBE_ISO=/path/to/minikube-sriov-amd64.iso # the naming is not relevant
$ make minikube-setup
```

#### SRIOV emulation walkthrough

Preparation: make sure you are in the project root and make sure to have the extra components handy. Build `virtinjectd`
```bash
$ make build-virtinjectd
```
Download the SRIOV-enabled custom ISO images from the [support repo](https://github.com/ffromani/dra-driver-integration-env-data/blob/main/minikube-sriov/README.md). **requires [git-lfs](https://git-lfs.com/)**.

In a separate terminal, run the virtinjectd proxy
```bash
$ ./build/bin/virtinjectd -hook scripts/libvirt-qemu-hook/dramachine.py -v=1
```
The proxy connects to libvirt, minikube must connect to the proxy. **Keep the proxy running while the minikube cluster is running**.

In the main terminal, point the machinery to the proxy socket:
```bash
$ export LIBVIRT_PROXY_SOCK=/tmp/libvirt-proxy.sock
```

Make the machinery aware we have the SRIOV-enabled custom image handy:
```bash
$ export MINIKUBE_ISO="$(pwd)/minikube-sriov-amd64.iso"
```

Now you can run the cluster setup command. **Hashes and addresses may vary on your setup, and this is not a problem per se**.
```bash
$ make minikube-setup
go build -v -o "/workspace/dra-driver-integration-env/build/bin/setup-runtime-containerd" ./setup/containerd
/workspace/dra-driver-integration-env/build/bin/setup-runtime-containerd -script > "/workspace/dra-driver-integration-env/build/bin/setup-runtime" && chmod 0755 "/workspace/dra-driver-integration-env/build/bin/setup-runtime"
go build -v -o "/workspace/dra-driver-integration-env/build/bin/setup-sriovvf" ./setup/sriovvf
#
# build steps omitted
#
minikube start \
	--feature-gates=DRAResourceClaimDeviceStatus=true,DRAConsumableCapacity=true,DRAPartitionableDevices=true \
	--iso-url='file:///workspace/dra-driver-integration-env/minikube-sriov-amd64.iso' \
	--container-runtime=containerd \
	--nodes=2 \
	--driver=kvm2 \
	--kvm-qemu-uri='qemu+unix:///system?socket=/tmp/libvirt-proxy.sock' \
	--kvm-numa-count=2 \
	--cpus=16 \
	--memory=16g
😄  minikube v1.38.0 on Fedora 43
    ▪ MINIKUBE_ISO=/workspace/dra-driver-integration-env/minikube-sriov-amd64.iso
✨  Using the kvm2 driver based on user configuration
👍  Starting "minikube" primary control-plane node in "minikube" cluster
🔥  Creating kvm2 VM (CPUs=16, Memory=16384MB, Disk=20000MB) ...
📦  Preparing Kubernetes v1.35.0 on containerd 2.2.1 ...
🔗  Configuring CNI (Container Networking Interface) ...
🔎  Verifying Kubernetes components...
    ▪ Using image gcr.io/k8s-minikube/storage-provisioner:v5
🌟  Enabled addons: default-storageclass, storage-provisioner

👍  Starting "minikube-m02" worker node in "minikube" cluster
🔥  Creating kvm2 VM (CPUs=16, Memory=16384MB, Disk=20000MB) ...
🌐  Found network options:
    ▪ NO_PROXY=192.168.72.139
📦  Preparing Kubernetes v1.35.0 on containerd 2.2.1 ...
    ▪ env NO_PROXY=192.168.72.139
🔎  Verifying Kubernetes components...
🏄  Done! kubectl is now configured to use "minikube" cluster and "default" namespace by default
kubectl label node minikube-m02 node-role.kubernetes.io/worker=''
node/minikube-m02 labeled
kubectl taint node minikube node-role.kubernetes.io/control-plane:NoSchedule
node/minikube tainted
minikube image load dev.kind.local/dra/dra-core-resource-drivers:v20260216-7cde3b7
minikube image load dev.kind.local/dra/dra-core-resource-validation:dev
scripts/wait-worker-nodes.sh
node/minikube condition met
node/minikube-m02 condition met
minikube cp build/bin/setup-sriovvf minikube-m02:/bin/setup-sriovvf
minikube ssh -n minikube-m02 sudo /bin/chmod 0755 /bin/setup-sriovvf
minikube ssh -n minikube-m02 sudo "/bin/setup-sriovvf -try -num-vfs=6 -verbosity=debug"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:00.0 reason="PCI device 0000:00:00.0 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.0 reason="PCI device 0000:00:01.0 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.1 reason="PCI device 0000:00:01.1 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.2 reason="PCI device 0000:00:01.2 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.3 reason="PCI device 0000:00:01.3 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.4 reason="PCI device 0000:00:01.4 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.5 reason="PCI device 0000:00:01.5 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.6 reason="PCI device 0000:00:01.6 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:01.7 reason="PCI device 0000:00:01.7 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:02.0 reason="PCI device 0000:00:02.0 is not a network interface device"
time=2026-02-16T10:06:38.391Z level=DEBUG msg="skipping PCI device" pci=0000:00:1f.0 reason="PCI device 0000:00:1f.0 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="skipping PCI device" pci=0000:00:1f.2 reason="PCI device 0000:00:1f.2 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="skipping PCI device" pci=0000:00:1f.3 reason="PCI device 0000:00:1f.3 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="skipping PCI device" pci=0000:01:00.0 reason="PCI device 0000:01:00.0 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="skipping PCI device" pci=0000:02:01.0 reason="PCI device 0000:02:01.0 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="skipping PCI device" pci=0000:03:00.0 reason="PCI device 0000:03:00.0 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="skipping PCI device" pci=0000:04:00.0 reason="PCI device 0000:04:00.0 is not a network interface device"
time=2026-02-16T10:06:38.392Z level=DEBUG msg="validated SR-IOV PF" pci=0000:05:00.0 total_vfs=7 current_vfs=0
time=2026-02-16T10:06:38.392Z level=INFO msg="selected SR-IOV PF device" pci=0000:05:00.0 path=/sys/bus/pci/devices/0000:05:00.0 total_vfs=7 current_vfs=0
time=2026-02-16T10:06:38.392Z level=INFO msg="configuring requested VFs" pci=0000:05:00.0 num_vfs=6
time=2026-02-16T10:06:39.719Z level=INFO msg="applying VF NUMA balancing" vf_count=6 numa_nodes="[0 1]"
time=2026-02-16T10:06:39.719Z level=DEBUG msg="setting VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.0 node=0
time=2026-02-16T10:06:39.721Z level=INFO msg="set VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.0 node=0
time=2026-02-16T10:06:39.721Z level=DEBUG msg="setting VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.2 node=1
time=2026-02-16T10:06:39.722Z level=INFO msg="set VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.2 node=1
time=2026-02-16T10:06:39.722Z level=DEBUG msg="setting VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.4 node=0
time=2026-02-16T10:06:39.723Z level=INFO msg="set VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.4 node=0
time=2026-02-16T10:06:39.723Z level=DEBUG msg="setting VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.6 node=1
time=2026-02-16T10:06:39.724Z level=INFO msg="set VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:10.6 node=1
time=2026-02-16T10:06:39.724Z level=DEBUG msg="setting VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:11.0 node=0
time=2026-02-16T10:06:39.726Z level=INFO msg="set VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:11.0 node=0
time=2026-02-16T10:06:39.726Z level=DEBUG msg="setting VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:11.2 node=1
time=2026-02-16T10:06:39.727Z level=INFO msg="set VF NUMA node" vf_path=/sys/devices/pci0000:00/0000:00:01.3/0000:05:11.2 node=1
time=2026-02-16T10:06:39.727Z level=INFO msg="try mode: operation succeeded"
minikube cp build/bin/setup-runtime-containerd minikube-m02:/bin/setup-runtime-containerd
minikube ssh -n minikube-m02 sudo /bin/chmod 0755 /bin/setup-runtime-containerd
minikube ssh -n minikube-m02 sudo /bin/setup-runtime-containerd /etc/containerd/config.toml
minikube ssh -n minikube-m02 sudo /bin/systemctl daemon-reload
minikube ssh -n minikube-m02 sudo /bin/systemctl restart containerd
kubectl create -f build/yaml/install.cpu.yaml
clusterrole.rbac.authorization.k8s.io/dracpu created
serviceaccount/dracpu created
clusterrolebinding.rbac.authorization.k8s.io/dracpu created
daemonset.apps/dracpu created
deviceclass.resource.k8s.io/dra.cpu created
kubectl create -f build/yaml/install.memory.yaml
clusterrole.rbac.authorization.k8s.io/dramemory created
serviceaccount/dramemory created
clusterrolebinding.rbac.authorization.k8s.io/dramemory created
daemonset.apps/dramemory created
deviceclass.resource.k8s.io/dra.memory created
deviceclass.resource.k8s.io/dra.hugepages-1g created
deviceclass.resource.k8s.io/dra.hugepages-2m created
kubectl create -f build/yaml/install.sriov.yaml
serviceaccount/sriov-dra-dra-driver-sriov-chart-service-account created
customresourcedefinition.apiextensions.k8s.io/sriovresourcefilters.sriovnetwork.k8snetworkplumbingwg.io created
clusterrole.rbac.authorization.k8s.io/sriov-dra-dra-driver-sriov-chart-role created
clusterrolebinding.rbac.authorization.k8s.io/sriov-dra-dra-driver-sriov-chart-role-binding created
daemonset.apps/drasriov created
deviceclass.resource.k8s.io/sriovnetwork.k8snetworkplumbingwg.io created
scripts/wait-resourcelices.sh
ready at try 4
```

The cluster is now ready. It can look like this:
```bash
$ kubectl get pods -A -o wide
NAMESPACE     NAME                               READY   STATUS    RESTARTS   AGE    IP               NODE           NOMINATED NODE   READINESS GATES
kube-system   coredns-7d764666f9-wt8fq           1/1     Running   0          162m   10.244.0.2       minikube       <none>           <none>
kube-system   dracpu-mbvnn                       1/1     Running   0          161m   192.168.72.210   minikube-m02   <none>           <none>
kube-system   dramemory-r29z2                    1/1     Running   0          161m   192.168.72.210   minikube-m02   <none>           <none>
kube-system   drasriov-5jmh7                     1/1     Running   0          161m   192.168.72.210   minikube-m02   <none>           <none>
kube-system   etcd-minikube                      1/1     Running   0          162m   192.168.72.139   minikube       <none>           <none>
kube-system   kindnet-ht9g9                      1/1     Running   0          162m   192.168.72.210   minikube-m02   <none>           <none>
kube-system   kindnet-sjcgq                      1/1     Running   0          162m   192.168.72.139   minikube       <none>           <none>
kube-system   kube-apiserver-minikube            1/1     Running   0          162m   192.168.72.139   minikube       <none>           <none>
kube-system   kube-controller-manager-minikube   1/1     Running   0          162m   192.168.72.139   minikube       <none>           <none>
kube-system   kube-proxy-9gwx7                   1/1     Running   0          162m   192.168.72.210   minikube-m02   <none>           <none>
kube-system   kube-proxy-xjmhd                   1/1     Running   0          162m   192.168.72.139   minikube       <none>           <none>
kube-system   kube-scheduler-minikube            1/1     Running   0          162m   192.168.72.139   minikube       <none>           <none>
kube-system   storage-provisioner                1/1     Running   0          162m   192.168.72.210   minikube-m02   <none>           <none>
```

To clean up the cluster, just run
```bash
make minikube-teardown
```

# Implementation design and notes

## why import `main.go` of the drivers?

To consume the latest and greatest code from the relevant drivers, decoupling from their release cycle, because some
key components are still experimental/early in development.
To efficiently preload driver images in the local kind cluster minimizing the network traffic.

Pulling the driver's `main.go`, alongside their install manifests (copy/paste them), enables us to easily
build a combined image and to load into the kind cluster.
The main upside of this approach is this don't require the drivers to provide pre-built up-to-date container images.
The main downside of this approach is we have to import (copy/paste) more from upstream drivers.
Rebuilding drivers locally in a combined image is a mean to an end, so in the future we will consider just pulling
pre-built images as main install vehicle or as alternative install vehicle.


# License

Apache v2
