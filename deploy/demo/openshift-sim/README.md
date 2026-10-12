# OpenShift benzetimi

Minikube'u Kubeat'in gözünde OpenShift'e benzetir. Ne kurduğu, neyi
doğruladığı ve neyi doğrulayamadığı ana README'deki "OpenShift" bölümünde.

```
make minikube-demo            # shop-demo ve monitoring-demo
make minikube-openshift-sim   # bu dizindeki install.sh + values.yaml ile kurulum
```

- `install.sh`: CRD'ler, thanos-querier taklidi, service CA, Route'lar.
- `values.yaml`: chart'ın OpenShift'teki değerleri (farklar içinde işaretli).
- `crds/`: OpenShift ve prometheus-operator'ın kendi CRD tanımları, olduğu gibi.
