# Helm chart authoring

## Debugging and testing
First of all build docker image from sources 

```bash
$ docker build . -f deploy/docker/Dockerfile -t latest
# Successfully built b40bbe9181a5
# Successfully tagged latest:latest
```

Let's use **'tfcluster'** as release name
Test helm chart
```bash
# for linting
$ helm lint deploy/helm/tfservingcache
# ...
# 1 chart(s) linted, 0 chart(s) failed

# for dry run
$ helm install tfcluster deploy/helm/tfservingcache --dry-run --debug
# will print rendered chart 
```
Install helm chart
```bash
# initial installation
$ helm install tfcluster deploy/helm/tfservingcache
# or upgrade
$ helm upgrade tfcluster deploy/helm/tfservingcache --install
```
Run curl in temporary pod 
```bash
# for interactive session
$ kubectl run --generator=run-pod/v1 curl --image=radial/busyboxplus:curl -i --tty --rm
# or to run curl command once
$ kubectl run --generator=run-pod/v1 curl --image=radial/busyboxplus:curl -i --tty --rm -- \
curl http://tfserving-tfservingcache:8094/v1/models/saved_model_half_plus_two_cpu/versions/00000123
```
Use node port service type
```bash
$ helm upgrade tfserving . --install \
--set models.provider.hostPath.path=/run/desktop/mnt/host/wsl/models \
--set service.type=NodePort \
--set logLevel=debug

# get service to obtain host 
$ kubectl get service -l app.kubernetes.io/instance=tfserving
# NAME                       TYPE       CLUSTER-IP     EXTERNAL-IP   PORT(S)                                                       AGE
# tfserving-tfservingcache   NodePort   10.106.18.87   <none>        8093:31230/TCP,8100:32460/TCP,8094:32767/TCP,8095:31991/TCP   12m
```

## Host Path Volume (WSL2)
The Kubernetes Volumes not correctly mounted with WSL2 (windows), here posible workaround 
Mount dirrectory with models
```bash 
mkdir /mnt/wsl/models
sudo mount --bind <path where models actualy located> /mnt/wsl/models
```
Pass mounted folder to kubernetes with `/run/desktop/mnt/host/wsl/` like so:
```bash 
helm install tfserving . --set models.provider.hostPath.path=/run/desktop/mnt/host/wsl/models
```
> Please read [Kubernetes Volumes not correctly mounted with WSL2](https://github.com/docker/for-win/issues/5325)

## TODO
- add liveness and readiness probes both for TFServing and TFServingCache
- add ingress rules (looks like the latest Nginx based ingress controller support both HTTP and GRPC)
- add support for Prometheus Operator
  - metrics from TFServing should be gathered also
  - build Grafana dashboard to show additional latency and resources consumption (will be very useful for future load testing and profiling )
