In this stage, you'll run the API, a worker and Postgres together with podman kube play.

A pod is the cluster's unit: containers that start together and share a network. `podman kube play` reads the same Kubernetes YAML the cluster does, so the whole app runs on your workstation before it goes anywhere near OpenShift.

### Expected behaviour

- `deploy/pod.yaml` holds one Pod, which may sit next to a ConfigMap in the same file. The Pod has three containers:
  - one that runs `serve`, with `ports: [{containerPort: ..., hostPort: ...}]`;
  - one that runs `work`;
  - one Postgres, from an image with `postgres` in its name.
- The API and the worker use the image `localhost/framewise:latest`, which is what `podman build -t framewise .` produces.
- Inside the pod, Postgres is on `localhost`. Postgres starts with the pod, so the app must wait for it and run `migrate` before it serves or works.
- `serve` gains `--host`, which defaults to `127.0.0.1`. In the pod it listens on `0.0.0.0`.
- The profiles come from a ConfigMap mounted as a file, and `FRAMEWISE_PROFILES` points at it.
- Uploads go to a volume that the API and the worker share.

### Tests

The tester builds your `Containerfile` as `localhost/framewise:latest`, then runs `podman kube play --replace deploy/pod.yaml`. The Postgres image must already be on the machine.

On the published host port, it waits for `/health`, then for `/ready`. Then it runs a job on a clip of a street; it must reach `DONE` with labels. Next it kills the worker's main process, as a crash would, and the pod must restart the worker within 60 s. Then a second job must reach `DONE`. At the end it runs `podman kube down --force`.

### Why your design just broke

Three things that never mattered on one machine matter now. First, startup order. Containers in a pod start together, and nothing waits for Postgres unless you make it. Second, network interfaces. A published port reaches the pod's network interface, so a server on `127.0.0.1` can't be reached. Third, configuration. The profiles file is no longer next to the code; it's mounted from a ConfigMap.

### Pointers

- [podman kube play](https://docs.podman.io/en/latest/markdown/podman-kube-play.1.html) and [podman kube down](https://docs.podman.io/en/latest/markdown/podman-kube-down.1.html).
- [Configure a Pod to Use a ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/).
- [Pod lifecycle: container restart policy](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#restart-policy).
- Search: `uvicorn host 0.0.0.0 container`, `kubernetes wait for database before start`.

Prepares for Framewise weeks 7 and 8.
