# Build your own Framewise

A CodeCrafters-style course for a junior backend developer, alongside the Framewise assignment
(`../framewise/ASSIGNMENTS.md`). In 23 stages, each checked by a black-box tester, the learner
grows a Python CLI into a video-analysis service: an API, a Postgres job queue with workers that
survive crashes, results queried in SQL, a model server called over Triton's HTTP protocol in
batches, structured logs and readiness, and API keys. The last four ship it: a GitLab pipeline
that gates merges, an image, a pod, and a Helm chart for OpenShift whose migrations run as a hook.

It is written for a backend engineer new to both infrastructure and video. Every stage has a
"New here?" list of the terms it uses and three levels of hints, from the idea to the shape of a
solution to the usual traps, each folded away until the learner opens it. Expect about 75 to 95
hours in all (the table below has an estimate per stage): about two and a half weeks full time, or
8 to 10 hours a week alongside the ten-week assignment.

Computer vision is a black box here, as it is for a backend engineer handed a model by a research
team. The starter ships one real model as an `.onnx` file, Ultralytics' YOLO26 nano detector
(`starter_templates/python/code/models/`), with a model card that is its whole contract: the
input tensor and how to prepare a frame for it, the output and which scores count. The learner
runs it with onnxruntime in-process, and later on a Triton-style model server, and never trains
or opens it. The learning is in everything around it. Each stage is chosen so the previous design breaks and has to be
refactored; the stage text says why, under "Why your design just broke".

Everything is in this one repo: the course, its tester (`tester/`) and the learner's attempt
(`attempts/python/`). The course layout is copied from
[codecrafters-io/build-your-own-http-server](https://github.com/codecrafters-io/build-your-own-http-server)
and the tester's from their tester-template; both are MIT, see `LICENSE` for CodeCrafters' notice.
It follows the one-repo protocol of [build-your-own-y](https://github.com/GuyLevavi/build-your-own-y).

## Do the course

With Nix (and direnv, or `nix develop`):

```sh
git clone https://github.com/GuyLevavi/build-your-own-framewise && cd build-your-own-framewise
./test.sh                # every stage up to the one in attempts/python/.stage
./test.sh --show         # the current stage's instructions; ./test.sh --show pb2 for any stage
./test.sh --next         # move .stage on once it passes
./test.sh pb2            # one stage
./test.sh --list         # every stage, * on the current one
```

Your code is `attempts/python/` (start in `app/main.py`); commit it on your own branch. `./test.sh`
enters the `python` dev shell (Python and every dependency from nixpkgs, ruff, py-spy, psql, helm),
builds the tester from `tester/` and runs it the way CodeCrafters does. Podman comes from the system.
The tester stops at the first failing stage.

Without Nix (the twentyx pod, or any machine with uv or podman), run the tester against any copy of
the starter:

```sh
cp -r compiled_starters/python ~/fw-attempt                         # or use attempts/python
FRAMEWISE_RUNNER=native tools/run_tests.sh ~/fw-attempt pb1        # stage 1, with uv on this machine
tools/run_tests.sh ~/fw-attempt wk2                                 # stages 1..10, in a container
tools/run_tests.sh ~/fw-attempt ap3 --only                          # just one stage
```

Each run re-runs every earlier stage, as CodeCrafters does. `FRAMEWISE_RUNNER=native` runs the
program with uv and Python 3.12 on the host (the twentyx pod); the default runs it in a container
built from `dockerfiles/`. The tester is built on first use if `go` is installed.

### Postgres

Stages from ap1 on need a Postgres server the tester may create databases on. Point
`FRAMEWISE_TEST_DATABASE_URL` at one (the team's hosted instance, see `TRAINEE.md` in framewise) and
the tester creates a `fw_test_*` database per stage and drops it afterwards. Unset, it starts its
own with podman. SQL in the solutions sticks to PostgreSQL 14 features; the default image is
`postgres:18`.

### The delivery stages (ci1 to hm1)

Stages 20 to 23 need `podman`, `helm` (hm1) and `git` on the machine, as the assignment's
workstation has. They run your pipeline's jobs in containers, build and run your image, play your
pod and render your chart, so in container mode `tools/run_tests.sh` runs them with the tester on
the host, after the other stages. The images they use: `FRAMEWISE_CI_IMAGE` (the CI job image) and
`FRAMEWISE_BASE_IMAGE` (the Containerfile's `BASE_IMAGE`) both default to
`ghcr.io/astral-sh/uv:python3.12-bookworm-slim`, and the pod's Postgres image must be loaded. On the
twentyx pod, which has no podman, run them on a workstation.

### Airgapped networks

`tools/make_release.sh` builds two tarballs: one for the learner (no solutions) and one for the
reviewer. Each holds the course, the tester as a static binary and as source with vendored Go
modules, every container image in `tools/images.env` (`images/load.sh` loads them into podman) and a
static helm (`bin/helm`, the flake's `.#helm`). Run it from `nix develop` with podman on the machine;
`IMAGES=0` or `HELM=` leave those out. The image names in `tools/images.env` are the defaults
everywhere (the tester, `.gitlab-ci.yml`), so nothing needs setting on either side. Python
dependencies come from the package index the network mirrors (onnxruntime, OpenCV, NumPy and the
rest; no PyTorch); the model is a file in the starter, so nothing is downloaded at run time.

## Stages

| # | Slug | Stage | What breaks | Hours |
| --- | --- | --- | --- | --- |
| 1 | pb1 | Probe a video |  | 1 |
| 2 | pb2 | Fail cleanly | errors become a type | 1.5 |
| 3 | an1 | Analyze a clip |  | 6 |
| 4 | an2 | Analyze a long clip | lists of frames → generators (memory budget) | 1.5 |
| 5 | an3 | Profiles in YAML | flags → validated config | 3 |
| 6 | ap1 | Upload a video | bodies streamed to disk | 3 |
| 7 | ap2 | Run jobs | analysis off the request path | 3 |
| 8 | ap3 | Survive a restart | in-memory state → Postgres repository, migrations (Alembic set up in the starter) | 8 |
| 9 | wk1 | Run workers | thread → `work` processes, the DB is the queue, `SKIP LOCKED` | 5 |
| 10 | wk2 | Crashed workers | claims get leases, heartbeats, attempts | 5 |
| 11 | wk3 | Stop gracefully | SIGTERM, give the job back | 2 |
| 12 | rs1 | Store detections | analysis split into frames and consumers; batches; an index | 4 |
| 13 | rs2 | Events in SQL | gaps-and-islands with window functions | 6 |
| 14 | rs3 | Retries without duplicates | idempotent writes; episodes keyed by what they are | 4 |
| 15 | md1 | Call a model server | where the model runs becomes a setting; the server's input size, FP32 tensors over HTTP | 6 |
| 16 | md2 | Send batches | one frame a request → batches of `max_batch_size` (fps and memory budget) | 3 |
| 17 | md3 | When the model server fails | timeouts, retry policy, failing a job clearly | 4 |
| 18 | op1 | Logs and readiness | JSON logs with context, fps per job, liveness vs readiness | 4 |
| 19 | hd1 | API keys | constant-time comparison, one auth layer | 1.5 |
| 20 | ci1 | Gate merges | "passes on my machine" → a pipeline on a clean clone | 4 |
| 21 | im1 | Build an image | a checkout as you → an image as a random UID, PID 1 | 4 |
| 22 | im2 | Run the pod | startup order, `--host`, config from a ConfigMap | 4 |
| 23 | hm1 | Chart it | one pod → Deployments, Service, Route, Secret, RWX claim; migrations as a Helm hook | 6 |

Hours are for a backend engineer new to the domain, using the hints; about 90 in all. Someone who
has run Postgres queues or Helm charts before goes much faster through those stages.

Cut from earlier versions of the course, because the assignment doesn't need them: input size as
a profile setting, multi-process analysis and broken process pools (pf1 to pf3), live progress
over SSE and LISTEN/NOTIFY, surviving a database outage (beyond readiness), Prometheus metrics,
keeping several model-server requests in flight, the classifier model, and releasing by a Git
commit that Argo CD syncs (the assignment covers Argo CD, with devops).

Performance targets are relative to the same machine (calibrated in the same test), so they hold on
a laptop, the pod or CI. The md stages' targets come from the tester's
own model server, which adds a fixed cost per request. That server is a stand-in for Triton (the
KServe v2 HTTP protocol, binary tensors, `max_batch_size` and `instance_group` in its config) that
runs the learner's own `models/yolo26n.onnx` with onnxruntime, so the learner meets Triton's
contract with the same model. Not covered here: training or evaluating a model, the evaluation gate,
pushing to Quay, the live cluster, Splunk and Argo CD. The delivery stages check everything up
to those: the pipeline's jobs, the image, the pod and the rendered chart. The assignment covers
the rest.

## Reviewing

`REVIEWING.md` has the reviewer's fast track (`./review.sh <slug>` starts any stage in
`attempts/python` from the previous reference solution), the per-stage brief (`tools/brief.py`: the task, what the reference
built, what to review and what the stage points to) and what to look for in each stage beyond
passing.

## Repo layout

| Path | What |
| --- | --- |
| `test.sh`, `review.sh` | the learner's and the reviewer's entry points (Nix) |
| `flake.nix`, `nix/python.nix` | the tester as `.#tester`; a dev shell per `nix/<lang>.nix` (`python`), and `default` (python + Go, for course work) |
| `attempts/python/` | the learner's code: the starter plus `.stage`; `tools/start.sh <lang>` starts another language |
| `tester/` | the tester: Go on CodeCrafters' tester-utils, `cmd/tester` and `internal/` (its own README) |
| `course-definition.yml` | slugs, names and order of the stages |
| `stage_descriptions/` | one Markdown file per stage: task, expected behaviour, tests, the refactor, pointers |
| `starter_templates/` | the source of the starter; `compiled_starters/python` is generated from it |
| `solutions/python/NN-slug/code` | reference solution for each stage (passes it and all earlier ones) |
| `solutions/python/NN-slug/diff` | what that stage changed in the learner's files (`app/`, and from ci1 `tests/`, `.gitlab-ci.yml`, `Containerfile`, `deploy/`) |
| `starter_templates/python/code/models/` | the `.onnx` detector, its labels and its model card |
| `dockerfiles/python-3.12.Dockerfile` | the image programs run in (container mode) |
| `tools/run_tests.sh` | run the tester against one directory, without Nix |
| `tools/verify_course.sh` | prove every stage: solution N passes 1..N, solution N-1 fails stage N |
| `tools/compile.sh` | regenerate the compiled starter, the solutions' boilerplate and the diffs |
| `tools/fast_track.sh` | the reviewer's fast track |
| `tools/brief.py` | the reviewer's brief for a stage |
| `tools/make_release.sh` | the airgap tarballs |
| `.github/workflows/ci.yml` | the checks on GitHub: generated files, briefs, the tester's tests, the flake and `./test.sh`, stages 1 to 5 (all 23 weekly) |
| `tools/images.env` | the container images the course uses, by default |
| `.gitlab-ci.yml` | the same pipeline for the airgapped GitLab: shell checks, `compile.sh` is clean, the tester's vet and unit tests, `verify_course.sh` |

## Checking the course itself

```sh
make -C tester build
FRAMEWISE_RUNNER=native tools/verify_course.sh        # all stages; logs in tools/.verify/
FRAMEWISE_RUNNER=native tools/verify_course.sh 9 15   # a range
tools/compile.sh                                      # after changing a solution or the starter
```

## Changing a stage

1. Edit `stage_descriptions/base-NN-slug.md` and the test in `tester/internal/stages_*.go`.
2. Update `solutions/python/NN-slug/code/app/` (and the later stages that build on it), then `tools/compile.sh`.
3. `make -C tester copy_course_file build && tools/verify_course.sh NN NN`.

Clips are generated at run time, in Go (`tester/internal/scenes.go`), from real photos of a person
and cars (`tester/internal/assets/`); what the model finds in every frame is recorded in
`tester/internal/golden/` by `tester/golden/make_golden.py`. Re-record after changing a scene or a
model (that script's header says how); `make -C tester test` checks the numbers the stage texts
quote.

## Local deviations from the starter

None: `attempts/python` is `compiled_starters/python` plus `.stage`. The starter's
`your_program.sh` already runs the Nix dev shell's Python when `FRAMEWISE_NIX_PYTHON` is set
(`nix/python.nix`), and `uv run` everywhere else.
