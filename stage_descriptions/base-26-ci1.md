In this stage, you'll write the GitLab pipeline that stops a broken merge request.

From here on, nobody merges by hand. A merge request goes into `main` only with a green pipeline, so the pipeline is the first reviewer of every change. These last five stages cover what the service needs to be shipped: they're checked with podman, helm and git on your machine, the same tools you use on the workstation.

### Expected behaviour

- `.gitlab-ci.yml` at the root of the repository, with `stages:` that list `lint` before `test`.
- At least one job in each. The lint job runs `ruff check` (and `ruff format --check`). The test job runs your own tests, kept under `tests/`.
- Every job up to the test stage runs on merge requests. A job kept to `main` by its `rules:` can't block anything.
- `UV_CACHE_DIR` points to a folder inside the project, such as `.uv-cache`, and `cache: paths:` lists it. Otherwise every pipeline downloads every package again from the mirror.
- The jobs take their image from `${CI_IMAGE}`, a project variable that names the CI image in the registry.

### Tests

The tester reads `.gitlab-ci.yml` and runs the jobs as a GitLab runner with the Docker executor would. Each job runs in its image with podman, on a fresh clone of your repository at `/builds/framewise`. Its `services:` run in the same pod, and its cache folders are kept between runs. `CI_IMAGE` is the uv and Python 3.12 image from the course tarball; set `FRAMEWISE_CI_IMAGE` to use your registry's.

It runs three pipelines, as merge requests:

1. Your code as it is. Every job up to the test stage must pass.
2. Your code plus `tests/test_planted_by_tester.py`, a test that fails. A job in the test stage must fail, and its output must name that test.
3. Your code plus `app/planted_by_tester.py`, which holds an unused import. A job in the lint stage must fail, and its output must name the file.

Not supported: `include:`, `extends:` (use YAML anchors) and `after_script:`. The tester doesn't evaluate `rules:`. It runs the jobs of every stage up to `test`.

### Why your design just broke

Until now "it passes" meant "it passed on my machine, today". A pipeline runs the same checks the same way for everyone, on a clean clone, so a forgotten file or an unpinned package shows up before review, not after the merge. Two things trip people up the first time. One: tools see the whole folder. Without the clone's `.gitignore`, `ruff check .` lints the uv cache too. Two: the order of the stages is a design choice, so cheap checks go first.

### Pointers

- [Create and run your first GitLab CI/CD pipeline](https://docs.gitlab.com/ci/quick_start/), then [Create a complex pipeline](https://docs.gitlab.com/ci/quick_start/tutorial/).
- [CI/CD YAML syntax reference](https://docs.gitlab.com/ci/yaml/): `stages`, `default`, `cache:key:files`, `services`, `rules`.
- [Using uv in GitLab CI/CD](https://docs.astral.sh/uv/guides/integration/gitlab/).
- Search: `gitlab merge request pipeline`, `protected branch pipelines must succeed`.
