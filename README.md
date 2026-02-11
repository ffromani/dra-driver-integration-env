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

TODO: document validation

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

To enable this functionality, you need to follow these steps **before** to create any minikube-based environments

1. copy `scripts/libvirt-qemu-hook/dramachine.py` in a system path of your choice. Example:
```bash
sudo install scripts/libvirt-qemu-hook/dramachine.py /usr/local/bin
```
2. set up the wrapper entry point in `/etc/libvirt/hooks/qemu`. If you already have hooks, use your hook manager to
add the `dramachine.py` hook. Otherwise you can use a sample wrapper like
```bash
#!/bin/bash
# update according to where you copied the dramachine.py in your system
/usr/local/bin/dramachine.py "$@" <&0
```
3. reload and restart libvirt
```bash
sudo systemctl daemon-reload
sudo systemctl restart libvirtd
```

Please refer to the hook [README](scripts/libvirt-qemu-hook/README.md) for the more implementation details and notes


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
