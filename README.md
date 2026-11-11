# Build your own Framewise

A CodeCrafters-style course for a junior backend developer, alongside the Framewise assignment
(`../framewise/ASSIGNMENTS.md`). In 30 stages, each checked by a black-box tester, the learner
grows a Python CLI into a video-analysis service: an API, a Postgres job queue with workers that
survive crashes, results queried in SQL, multi-process performance work, a model server called
over Triton's HTTP protocol, metrics, live progress and outage handling. The last five ship it: a
GitLab pipeline that gates merges, an image, a pod, a Helm chart and a release that is a Git commit.

Computer vision is a black box here. The detector, `fwdetect`, is a small NumPy wheel in
`packages/fwdetect/` with a written contract: three labels, a cost that grows with pixels and holds
the GIL, a trace file for the tester, and a frame that crashes the process on purpose. The learning
is in everything around it. Each stage is chosen so the previous design breaks and has to be
refactored; the stage text says why, under "Why your design just broke".

Layout copied from [codecrafters-io/build-your-own-http-server](https://github.com/codecrafters-io/build-your-own-http-server);
the tester is [framewise-tester](https://github.com/GuyLevavi/framewise-tester) (clone it next to this repo).
Both are MIT; see `LICENSE` for CodeCrafters' notice. Learners with Nix can work in
[codecrafters-framewise-python](https://github.com/GuyLevavi/codecrafters-framewise-python), which pins both
as flake inputs (the protocol of [build-your-own-y](https://github.com/GuyLevavi/build-your-own-y)).

## Try it as a learner

```sh
cp -r compiled_starters/python ~/fw-attempt                         # your repo; start in app/main.py
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

### The delivery stages (ci1 to hm2)

Stages 26 to 30 need `podman`, `helm` (hm1, hm2) and `git` on the machine, as the assignment's
workstation has. They run your pipeline's jobs in containers, build and run your image, play your
pod and render your chart, so in container mode `tools/run_tests.sh` runs them with the tester on
the host, after the other stages. The images they use: `FRAMEWISE_CI_IMAGE` (the CI job image) and
`FRAMEWISE_BASE_IMAGE` (the Containerfile's `BASE_IMAGE`) both default to
`ghcr.io/astral-sh/uv:python3.12-bookworm-slim`, and the pod's Postgres image must be loaded. On the
twentyx pod, which has no podman, run them on a workstation.

### Airgapped networks

`tools/make_release.sh` builds two tarballs: one for the learner (no solutions) and one for the
reviewer. Each holds the course, the tester as a static binary and as source with vendored Go
modules, and with `IMAGES=1` the two container images (`postgres:18`,
`ghcr.io/astral-sh/uv:python3.12-bookworm-slim`), and with `HELM=/path/to/helm` a static helm binary.
Python dependencies come from the package index the network mirrors; `fwdetect` is vendored in the
starter as a wheel.

## Stages

| # | Slug | Stage | What breaks |
| --- | --- | --- | --- |
| 1 | pb1 | Probe a video | |
| 2 | pb2 | Fail cleanly | errors become a type |
| 3 | an1 | Analyze a clip | |
| 4 | an2 | Analyze a long clip | lists of frames → generators (memory budget) |
| 5 | an3 | Profiles in YAML | flags → validated config |
| 6 | ap1 | Upload a video | bodies streamed to disk |
| 7 | ap2 | Run jobs | analysis off the request path |
| 8 | ap3 | Survive a restart | in-memory state → Postgres repository, migrations |
| 9 | wk1 | Separate the worker | thread → `work` process, the DB is the queue |
| 10 | wk2 | Many workers | `FOR UPDATE` → `SKIP LOCKED` |
| 11 | wk3 | Crashed workers | claims get leases, heartbeats, attempts |
| 12 | wk4 | Stop gracefully | SIGTERM, give the job back |
| 13 | rs1 | Store detections | analysis split into frames and consumers; batches; an index |
| 14 | rs2 | Events in SQL | gaps-and-islands with window functions |
| 15 | rs3 | Retries without duplicates | idempotent writes; episodes keyed by what they are |
| 16 | pf1 | Downscale | one function owns detector size (fps budget) |
| 17 | pf2 | Use every core | process pool over frame ranges (speed-up and memory budget) |
| 18 | pf3 | When a process dies | `Pool` hangs; executor + attempts |
| 19 | md1 | Call a model server | the detector becomes a setting; RGB, model size, its own contract |
| 20 | md2 | Batch and pipeline | one frame at a time → batches in flight, with backpressure (fps and memory budget) |
| 21 | md3 | When the model server fails | timeouts, retry policy, failing a job clearly |
| 22 | op1 | Metrics and logs | Prometheus, fps per job, JSON logs with context |
| 23 | op2 | Live progress | SSE + LISTEN/NOTIFY |
| 24 | op3 | Survive a database outage | liveness vs readiness, 503s, backoff |
| 25 | hd1 | API keys | constant-time comparison, one auth layer |
| 26 | ci1 | Gate merges | "passes on my machine" → a pipeline on a clean clone |
| 27 | im1 | Build an image | a checkout as you → an image as a random UID, PID 1 |
| 28 | im2 | Run the pod | startup order, `--host`, config from a ConfigMap |
| 29 | hm1 | Chart it | one pod → Deployments, Service, Route, Secret, RWX claim |
| 30 | hm2 | Release by commit | `helm upgrade` by hand → a commit Argo CD syncs |

Performance targets are relative to the same machine (calibrated in the same test), so they hold on
a laptop, the pod or CI; pf2 needs at least 2 CPUs. The md stages' targets come from the tester's
own model server, whose cost per request is fixed. That server is a stand-in for Triton (the KServe
v2 HTTP protocol, binary tensors, `max_batch_size` and `instance_group` in its config) with the same
coloured-box rule as fwdetect, so the learner meets Triton's contract before the real one. Not
covered here: YOLO itself and its pre- and post-processing, the evaluation gate, pushing to Quay,
the live cluster, Splunk and Argo CD's sync. The delivery stages check
everything up to those: the pipeline's jobs, the image, the pod, the rendered chart and the release
commit. The assignment covers the rest.

## Reviewing

`REVIEWING.md` has the reviewer's fast track (`tools/fast_track.sh` starts any stage from the
previous reference solution), the per-stage brief (`tools/brief.py`: the task, what the reference
built, what to review and what the stage points to) and what to look for in each stage beyond
passing.

## Repo layout

| Path | What |
| --- | --- |
| `course-definition.yml` | slugs, names and order of the stages |
| `stage_descriptions/` | one Markdown file per stage: task, expected behaviour, tests, the refactor, pointers |
| `starter_templates/` | the source of the starter; `compiled_starters/python` is generated from it |
| `solutions/python/NN-slug/code` | reference solution for each stage (passes it and all earlier ones) |
| `solutions/python/NN-slug/diff` | what that stage changed in the learner's files (`app/`, and from ci1 `tests/`, `.gitlab-ci.yml`, `Containerfile`, `deploy/`) |
| `packages/fwdetect/` | the detector, built into the wheel in `starter_templates/python/code/vendor/` |
| `dockerfiles/python-3.12.Dockerfile` | the image programs run in (container mode) |
| `tools/run_tests.sh` | run the tester against one directory |
| `tools/verify_course.sh` | prove every stage: solution N passes 1..N, solution N-1 fails stage N |
| `tools/compile.sh` | regenerate the compiled starter, the solutions' boilerplate and the diffs |
| `tools/fast_track.sh` | the reviewer's fast track |
| `tools/brief.py` | the reviewer's brief for a stage |
| `tools/make_release.sh` | the airgap tarballs |
| `.gitlab-ci.yml` | this repo's own pipeline: shell checks, `compile.sh` is clean, `verify_course.sh` |

## Checking the course itself

```sh
make -C ../framewise-tester build
FRAMEWISE_RUNNER=native tools/verify_course.sh        # all stages; logs in tools/.verify/
FRAMEWISE_RUNNER=native tools/verify_course.sh 9 15   # a range
tools/compile.sh                                      # after changing a solution or the starter
```

## Changing a stage

1. Edit `stage_descriptions/base-NN-slug.md` and the test in `../framewise-tester/internal/stages_*.go`.
2. Update `solutions/python/NN-slug/code/app/` (and the later stages that build on it), then `tools/compile.sh`.
3. `make -C ../framewise-tester copy_course_file build && tools/verify_course.sh NN NN`.

Clips are generated at run time, in Go (`../framewise-tester/internal/scenes.go`); `make test` there
checks the numbers the stage texts quote. Rebuild the wheel after changing `fwdetect`
(`uv build packages/fwdetect` and copy it into the starter's `vendor/`).
