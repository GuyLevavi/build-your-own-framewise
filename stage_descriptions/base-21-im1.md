In this stage, you'll package the service as one small image that runs as any user.

The cluster runs images, not checkouts. One image holds the API, the worker and the migrations. The command picks which one runs, so all three are always the same build.

### New here?

- **Image**: a packaged filesystem plus a command, built from a `Containerfile`. A **container** is a running image.
- **Multi-stage build**: one stage with the build tools installs everything; the final stage copies over only what runs, so tools and caches stay out.
- **Random UID**: OpenShift runs containers as a user ID nobody chose (such as 1000620000) in group 0, so files must be writable by group 0, not by a named user.
- **PID 1**: the first process in a container. Signals such as SIGTERM go to it; if it's a shell that doesn't pass them on, your program never hears them.

### Expected behaviour

- A `Containerfile` (or `Dockerfile`) at the root, with at least two stages. The build stage installs the dependencies with uv. The runtime stage copies over only the virtualenv and `app/`.
- It builds `FROM ${BASE_IMAGE}`, a build argument that names the registry's Python base image.
- A `.containerignore` that keeps `.git`, `.venv`, `tests/` and `.env` out of the build context.
- The image's `ENTRYPOINT` is your program. `podman run IMAGE serve --port N --data-dir DIR`, `... work` and `... migrate` behave like `./your_program.sh serve ...` and the rest.
- It runs as a random user ID in group 0, the way OpenShift starts containers. Everything the app writes, `WORKDIR` included, belongs to group 0 and is group-writable.

### Tests

The tester copies your repository and adds `.env`, `.git/tester-planted`, `.venv/tester-planted` and `tests/tester-planted.py` to the copy. It then builds the image with `podman build --build-arg BASE_IMAGE=...`. By default the base is the uv and Python 3.12 image from the course tarball; set `FRAMEWISE_BASE_IMAGE` to use your own.

It then exports the image's filesystem and checks three things. None of the planted files are there. There's no `gcc`, `cc`, `make` or `ld`. `WORKDIR` belongs to group 0 and is group-writable.

Then it runs the image with `--user 1000620000:0 --network host`, mounting its profiles file at `/etc/framewise-tester/profiles.yaml`. First `migrate` must exit 0. Then it starts `serve --port N --data-dir /data` and `work`, with a shared volume at `/data`, and runs a job end to end. Last, `podman stop` on the worker must end with exit code 0.

### Why your design just broke

On your machine the program runs as you, from a virtualenv that `uv run` keeps up to date. In the cluster it runs as a user nobody chose, in a read-mostly filesystem. Anything that writes at startup fails there: syncing a virtualenv, creating a cache in the home folder, writing next to the code. A single-stage build also ships uv's download cache and every build tool. Your program also becomes PID 1. A shell-form `ENTRYPOINT`, or a wrapper script without `exec`, swallows the SIGTERM that the graceful shutdown from wk3 depends on.

### Pointers

- [Using uv in Docker](https://docs.astral.sh/uv/guides/integration/docker/): multi-stage builds and `UV_COMPILE_BYTECODE`.
- [Multi-stage builds](https://docs.docker.com/build/building/multi-stage/) and [building best practices](https://docs.docker.com/build/building/best-practices/). They apply to Podman unchanged.
- OpenShift docs, [Creating images](https://docs.redhat.com/en/documentation/openshift_container_platform/4.16/html/images/creating-images), section "Support arbitrary user ids".
- Search: `docker entrypoint exec form pid 1 signals`, `chgrp -R 0 chmod g=u openshift`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Build stage: start from `${BASE_IMAGE}`, copy `pyproject.toml` and `uv.lock`, and `uv sync --locked --no-dev` into a virtualenv outside the code. Runtime stage: same base, copy the virtualenv, `app/` and `models/`, make the folders the app writes group 0 and group-writable, put the virtualenv on `PATH`, and set the program as the exec-form `ENTRYPOINT`.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

```dockerfile
ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS build
WORKDIR /src
ENV UV_PROJECT_ENVIRONMENT=/opt/venv UV_LINK_MODE=copy UV_COMPILE_BYTECODE=1
COPY pyproject.toml uv.lock ./
RUN uv sync --locked --no-dev --no-install-project

FROM ${BASE_IMAGE}
WORKDIR /app
COPY --from=build /opt/venv /opt/venv
COPY app app
COPY models models
RUN mkdir -p /data && chgrp -R 0 /app /data && chmod -R g=u /app /data
ENV PATH=/opt/venv/bin:$PATH
USER 1001
ENTRYPOINT ["python", "-m", "app.main"]
```

And a `.containerignore` listing `.git`, `.venv`, `tests/`, `.env`.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`ENTRYPOINT python -m app.main` (shell form) runs under `/bin/sh -c`, which swallows SIGTERM: use the JSON form. `uv run` at startup tries to sync the venv and fails as a random user: call the venv's `python` directly. Check the base image doesn't ship gcc in the runtime stage.

</details>
