DRA driver integration environment
==================================

A testbench and local lab to try integration of multiple DRA drivers.
This lab focuses on the Network Function flows, and include example workloads
which exercise DRA-backed CPU, Memory and SRIOV network device allocation.

# Requirements

* golang >= 1.25
* kubectl (or equivalent) to interact with the kubernetes cluster
* [kind](https://kind.sigs.k8s.io/) >= 0.31

# Quickstart

First, you can use `make help` to get a list of the supported targets.
A fair amount of targets is used internally, the following quickstart
will illustrate the more useful for humans.

Set up the kind cluster:
```bash
make kind-setup
```

Once the step completes succesfully, the kind cluster would look like this
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

Now we can run the validation tool. We can start checking the alignment of
one of the pods which require resource alignment (random pick)
```bash
$ ./build/bin/validate-alignment default pod-lowlat-minimal-tclh5
Pod: default/pod-lowlat-minimal-tclh5 QoS=Guaranteed running on node: dra-core-resource-drivers-worker (Docker ID: dda57c9c32aa)
================================================================================================================================
+ Container: workload-container
  + Memory:
    + NUMA Zone 0
  + CPU:
    + NUMA Zone 0: CPUs 1

```

and compare with the reference pod which does not allocate resources
```bash
$ ./build/bin/validate-alignment default reference-burstable
Pod: default/reference-burstable QoS=Burstable running on node: dra-core-resource-drivers-worker (Docker ID: dda57c9c32aa)
==========================================================================================================================
+ Container: reference
  + Memory:
    + NUMA Zone 0
  + CPU:
    + NUMA Zone 0: CPUs 3-15,18-31

```

To remove the kind cluster:
```bash
make kind-teardown
```

# License

Apache v2
