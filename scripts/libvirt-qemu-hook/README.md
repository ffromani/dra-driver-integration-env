# dramachine libvirt hook

this hook augments the base minikube VM definition to support
intel IGB emulated devices, which in turn enable SRIOV emulation.

Details: https://docs.openstack.org/nova/latest/contributor/testing/pci-passthrough-sriov.html
