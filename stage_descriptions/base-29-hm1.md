In this stage, you'll write the Helm chart that runs Framewise in OpenShift.

A pod on a workstation becomes a set of objects in the cluster: Deployments that keep replicas running, a Service and a Route that reach them, a ConfigMap and a Secret for configuration, and a claim on shared storage. A Helm chart holds them as templates. Each environment gets a values file.

### Expected behaviour

The chart lives in `deploy/chart/`, with `Chart.yaml`, `values.yaml` and `values-nonprod.yaml`. `image.tag` is set in `values-nonprod.yaml`. Rendered with `values-nonprod.yaml`, it produces the following:

- **Workloads:** one Deployment whose container runs `serve`, another that runs `work`, and a Job that runs `migrate`. All of them use the image tag from `image.tag`.
- **API container:**
  - `serve` listens on `0.0.0.0`;
  - `livenessProbe` is an `httpGet` on `/health`, and `readinessProbe` is an `httpGet` on `/ready`;
  - it sets `resources.requests.cpu` and `resources.limits.memory`, as the worker does.
- **Networking:** a Service selects the API pods and not the worker's, and targets serve's `--port`. A Route (`route.openshift.io/v1`) points to that Service.
- **Secrets:** `DATABASE_URL` comes from a Secret through `valueFrom.secretKeyRef`, in every container that runs your program. No database URL appears anywhere in the rendered output, and nothing sets `runAsUser`.
- **Config:** `FRAMEWISE_PROFILES` points into a mounted ConfigMap that the chart renders.
- **Storage:** serve's `--data-dir` is on a PersistentVolumeClaim with `ReadWriteMany`, and the worker mounts the same claim at the same path.

### Tests

The tester runs `helm lint deploy/chart -f deploy/chart/values-nonprod.yaml`, then `helm template fw deploy/chart -f deploy/chart/values-nonprod.yaml --set image.tag=tester-NNNNNN`. It checks the rendered objects against the list above. Nothing is installed: the cluster part is week 8 of the assignment, with the tutor.

### Why your design just broke

The pod from im2 hard-coded everything: the database password, a hostPort, an emptyDir that vanishes with the pod. In a cluster, each of those becomes its own object, owned by someone else and changed at its own pace. A Secret is created outside Git. Storage outlives pods. Several workers on different nodes share one volume. The probes decide when Kubernetes restarts a pod or stops sending it traffic. A liveness probe that checks the database turns a database outage into every pod restarting.

### Pointers

- [Helm Chart Template Guide](https://helm.sh/docs/chart_template_guide/getting_started/), through values files and named templates.
- [Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).
- [Persistent volumes: access modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes) and [Secrets as environment variables](https://kubernetes.io/docs/concepts/configuration/secret/#using-secrets-as-environment-variables).
- Search: `openshift route edge termination`, `helm checksum configmap annotation restart`.

Prepares for Framewise week 8.
