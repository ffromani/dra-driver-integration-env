manifests generated with
```bash
cd https://github.com/k8snetworkplumbingwg/dra-driver-sriov
helm template sriov-dra ./deployments/helm/dra-driver-sriov/ \
  --create-namespace \
  -n dra-driver-sriov > install.tmpl.yaml
```
