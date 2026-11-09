# Build your own Framewise

A compact, CodeCrafters-style version of the Framewise ten-week assignment
(`../framewise/ASSIGNMENTS.md`): 19 small stages, each checked by a black-box tester, that take a
learner from an empty `main.py` to a hardened video-analysis API. Everything runs locally in
podman; no CodeCrafters account or server is involved.

Layout copied from [codecrafters-io/build-your-own-http-server](https://github.com/codecrafters-io/build-your-own-http-server);
the tester is [framewise-tester](https://github.com/GuyLevavi/framewise-tester) (clone it next to this repo
for the tools below). Both are MIT; see `LICENSE` for CodeCrafters' notice. Learners work in
[codecrafters-framewise-python](https://github.com/GuyLevavi/codecrafters-framewise-python), which pins both
as flake inputs and runs `./test.sh` with Python from nixpkgs (the protocol of
[build-your-own-y](https://github.com/GuyLevavi/build-your-own-y)).
Stage texts are written for this course, not copied.

## Try it as a learner

```sh
cp -r compiled_starters/python ~/fw-attempt         # your repo; edit app/main.py
tools/run_tests.sh ~/fw-attempt pb1                 # stage 1 (first run builds the image: ~1-2 min)
tools/run_tests.sh ~/fw-attempt mo2                 # stages 1..7
tools/run_tests.sh ~/fw-attempt ap3 --only          # just one stage
```

Read the stages in order in `stage_descriptions/`. Each run re-runs every earlier stage, as
CodeCrafters does. Only podman (or docker) is needed on the host: the program runs in a
container with Python 3.13, OpenCV, FastAPI, Uvicorn and PyYAML, and your code mounted at
`/app`. The tester is built on first use (Go via `nix shell` if `go` isn't installed).

To run your program by hand on NixOS, use the same container:

```sh
podman run --rm -it -v ~/fw-attempt:/app -w /app "$(podman images -q framewise-deps | head -1)" \
  uv run -m app.main probe some.avi
```

## Stages

| # | Slug | Stage | Framewise week |
| --- | --- | --- | --- |
| 1 | pb1 | Print the frame size | 1: probe CLI |
| 2 | pb2 | Report frame rate and length | 1 |
| 3 | pb3 | Handle a missing file | 1 |
| 4 | pb4 | Handle a corrupt file | 1 |
| 5 | pb5 | Save sample frames | 1 |
| 6 | mo1 | Detect motion | 2: analysis core |
| 7 | mo2 | Split motion into segments | 2 |
| 8 | mo3 | Bridge short pauses | 2 |
| 9 | mo4 | Ignore sensor noise | 2 |
| 10 | cf1 | Named detectors in YAML | 2: variants in YAML |
| 11 | cf2 | Reject a bad config | 2 |
| 12 | ap1 | Serve a health check | 3: HTTP API |
| 13 | ap2 | Upload a video | 3 |
| 14 | ap3 | Run a job | 3 |
| 15 | ap4 | Report job errors | 3 |
| 16 | ap5 | Run jobs in the background | 4: separate worker |
| 17 | ap6 | Survive a restart | 4: persistence (SQLite here, Postgres there) |
| 18 | hd1 | Require an API key | 10: hardening |
| 19 | hd2 | Limit uploads | 10 |

Left out because they can't be black-box tested on one PC without the internal services: YOLO
and Triton (week 5), the evaluation gate (6), images, OpenShift and Argo CD (7-9), and the
events SQL (10). The full assignment still covers them.

## Repo layout

| Path | What |
| --- | --- |
| `course-definition.yml` | slugs, names and order of the stages |
| `stage_descriptions/` | one Markdown file per stage: task, exact I/O, how it's tested, hints |
| `starter_templates/` | the source of the starter; `compiled_starters/python` is generated from it |
| `solutions/python/NN-slug/code` | reference solution for each stage (passes it and all earlier ones) |
| `packages/fwdetect/` | the detector shipped to learners as a wheel (the redesigned course's black box) |
| `dockerfiles/python-3.13.Dockerfile` | the image programs run in |
| `tools/run_tests.sh` | run the tester against one directory |
| `tools/verify_course.sh` | prove every stage: solution N passes 1..N, solution N-1 fails stage N |

## Checking the course itself

```sh
tools/verify_course.sh                 # all 19 stages, ~10 min at 0.5 CPU; logs in tools/.verify/
../build-your-own-y/tools/course-sdk compile python   # regenerate compiled_starters/ and the solution diffs
../build-your-own-y/tools/course-sdk test python      # course-sdk's own check: Dockerfile, starter, every solution
```

[build-your-own-y](https://github.com/GuyLevavi/build-your-own-y)'s `tools/course-sdk` is course-sdk with an offline patch that makes it use the local tester instead of downloading a
release, add the Dockerfile's generated sections itself instead of calling CodeCrafters' backend,
and accept podman's build output. `tools/registries.conf` lets podman resolve the short image
names course-sdk uses.

`nix develop` gives Go, Bun, make and podman; here Python only runs in containers. (The solution repo
runs it from nixpkgs instead.)

## Changing a stage

1. Edit `stage_descriptions/base-NN-slug.md` and the test in `../framewise-tester/internal/stages_*.go`.
2. Update `solutions/python/NN-slug/code/app/` (and the later stages that build on it).
3. `make -C ../framewise-tester build && tools/verify_course.sh NN NN`.

New clips: edit `../framewise-tester/internal/clips.go` (they are generated at run time, in Go), and
check `make test` there still agrees with the times quoted in the stage texts.
