In this stage, you'll write the Helm chart that runs Framewise in OpenShift.

A pod on a workstation becomes a set of objects in the cluster: Deployments that keep replicas running, a Service and a Route that reach them, a ConfigMap and a Secret for configuration, and a claim on shared storage. A Helm chart holds them as templates. Each environment gets a values file.

### New here?

- **Helm chart**: Kubernetes YAML with placeholders (`{{ .Values.image.tag }}`), filled from values files. One chart, one values file per environment.
- **Deployment**: keeps N copies (replicas) of a pod running. **Service**: one stable address in front of a Deployment's pods. **Route**: OpenShift's way to reach a Service from outside.
- **Secret**: like a ConfigMap, for passwords; created outside Git and read through `secretKeyRef`.
- **PersistentVolumeClaim (PVC)**: a request for storage that outlives pods. `ReadWriteMany` lets pods on different nodes share it.
- **Helm hook**: a template Helm runs at a set moment of a release instead of with everything else, such as a Job before an upgrade.

### Expected behaviour

The chart lives in `deploy/chart/`, with `Chart.yaml`, `values.yaml` and `values-nonprod.yaml`. `image.tag` is set in `values-nonprod.yaml`. Rendered with `values-nonprod.yaml`, it produces the following:

- **Workloads:** one Deployment whose container runs `serve`, another that runs `work`, and a Job that runs `migrate`. All of them use the image tag from `image.tag`.
- **Migrations first:** the migrate Job is a Helm hook, `helm.sh/hook: pre-install,pre-upgrade`, so every install and upgrade changes the schema before it starts new pods.
- **API container:**
  - `serve` listens on `0.0.0.0`;
  - `livenessProbe` is an `httpGet` on `/health`, and `readinessProbe` is an `httpGet` on `/ready`;
  - it sets `resources.requests.cpu` and `resources.limits.memory`, as the worker does.
- **Networking:** a Service selects the API pods and not the worker's, and targets serve's `--port`. A Route (`route.openshift.io/v1`) points to that Service.
- **Secrets:** `DATABASE_URL` comes from a Secret through `valueFrom.secretKeyRef`, in every container that runs your program. No database URL appears anywhere in the rendered output, and nothing sets `runAsUser`.
- **Config:** `FRAMEWISE_PROFILES` points into a mounted ConfigMap that the chart renders.
- **Storage:** serve's `--data-dir` is on a PersistentVolumeClaim with `ReadWriteMany`, and the worker mounts the same claim at the same path.

### Tests

The tester runs `helm lint deploy/chart -f deploy/chart/values-nonprod.yaml`, then `helm template fw deploy/chart -f deploy/chart/values-nonprod.yaml --set image.tag=tester-NNNNNN`. It checks the rendered objects against the list above. Nothing is installed: the cluster part is in the assignment, with the tutor.

### Why your design just broke

The pod from im2 hard-coded everything: the database password, a hostPort, an emptyDir that vanishes with the pod. In a cluster, each of those becomes its own object, owned by someone else and changed at its own pace. A Secret is created outside Git. Storage outlives pods. Several workers on different nodes share one volume. The probes decide when Kubernetes restarts a pod or stops sending it traffic. A liveness probe that checks the database turns a database outage into every pod restarting. And order matters again, across releases this time: a migration that runs after the new pods start serves new code against the old schema.

### Pointers

- [Helm Chart Template Guide](https://helm.sh/docs/chart_template_guide/getting_started/), through values files and named templates.
- [Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).
- [Chart hooks](https://helm.sh/docs/topics/charts_hooks/) (`pre-install`, `pre-upgrade`, `hook-delete-policy`).
- [Persistent volumes: access modes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#access-modes) and [Secrets as environment variables](https://kubernetes.io/docs/concepts/configuration/secret/#using-secrets-as-environment-variables).
- Search: `openshift route edge termination`, `helm checksum configmap annotation restart`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to explain why liveness and readiness use different endpoints, what makes the pods restart when only the ConfigMap changes, and what would go wrong if migrations weren't a hook that runs before the release.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Take im2's pod apart: each container becomes a Deployment (api, worker), migration becomes a Job, the pod's hard-coded values become `values.yaml` entries, the password becomes a Secret reference, the `emptyDir` becomes a PVC, and the hostPort becomes a Service plus a Route.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

Files under `deploy/chart/`: `Chart.yaml`, `values.yaml`, `values-nonprod.yaml`, `templates/_helpers.tpl` (a `framewise.image` helper), `templates/api.yaml` (Deployment, Service, Route), `templates/worker.yaml`, `templates/migrate-job.yaml`, `templates/configmap.yaml` (profiles from values), `templates/storage.yaml` (the PVC). In each container:

```yaml
env:
  - name: DATABASE_URL
    valueFrom:
      secretKeyRef: {name: {{ .Values.database.secret }}, key: url}
```

The Service's `selector` must match the API pods' labels and not the worker's (give them different `app.kubernetes.io/component` labels). The migrate Job's `metadata.annotations` get `helm.sh/hook: pre-install,pre-upgrade` and `helm.sh/hook-delete-policy: before-hook-creation,hook-succeeded`, so a finished Job doesn't block the next release.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

Run `helm template fw deploy/chart -f deploy/chart/values-nonprod.yaml` after every change and read the output: it's what the tester reads. Don't set `runAsUser` (OpenShift picks it). The liveness probe is `/health`, never `/ready`.

</details>
