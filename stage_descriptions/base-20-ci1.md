In this stage, you'll write the GitLab pipeline that stops a broken merge request.

From here on, nobody merges by hand. A merge request goes into `main` only with a green pipeline, so the pipeline is the first reviewer of every change. The last four stages cover what the service needs to be shipped: they're checked with podman, helm and git on your machine, the same tools you use on the workstation.

### New here?

- **Pipeline**: the checks GitLab runs on every merge request, defined in `.gitlab-ci.yml`. Merging is blocked until they pass.
- **Stage / job**: a pipeline is stages in order (`lint`, `test`); each stage holds jobs that run in their own fresh container.
- **Cache**: folders GitLab keeps between pipelines, so packages aren't downloaded every time.
- **pytest**: Python's test runner. It finds files named `test_*.py` and runs functions named `test_*`.

### Expected behaviour

- `.gitlab-ci.yml` at the root of the repository, with `stages:` that list `lint` before `test`.
- At least one job in each. The lint job runs `ruff check` (and `ruff format --check`). The test job runs your own tests, kept under `tests/`.
- Your code as it stands must pass that lint. The ruff that `uv.lock` pins (0.17.4) checks far more by default than unused imports: among others, import order (I001), a `try` that only re-raises (TRY203), and a blind `except Exception` that neither logs the traceback nor re-raises (BLE001). Run `uv run ruff check .` and `uv run ruff format --check .` on your 19 stages of code before you write the pipeline, so the findings don't arrive all at once in CI.
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

### In review

The tester can't check this part, so a reviewer will: you'll be asked to say where the pipeline would get its packages on the air-gapped network, and to show that your tests test logic rather than exist for the gate.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Two stages, cheap first. The lint job runs ruff; the test job runs pytest on your own tests. Both run in the CI image, install your dependencies with uv into a cached folder inside the project, and run on merge requests.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

```yaml
stages: [lint, test]
default:
  image: ${CI_IMAGE}
  before_script:
    - uv sync --locked
variables:
  UV_CACHE_DIR: .uv-cache   # inside the project, so the runner can cache it
  UV_LINK_MODE: copy
cache:
  key:
    files: [uv.lock]
  paths: [.uv-cache]
lint:
  stage: lint
  script:
    - uv run ruff check .
    - uv run ruff format --check .
test:
  stage: test
  script:
    - uv run python -m pytest -q tests   # -m puts the project on sys.path
```

Then write a few tests under `tests/` for pure functions you already have: segments from sightings, profile validation.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`ruff check .` also lints `.uv-cache` (third-party code, full of findings) unless `.gitignore` lists it, since ruff skips what Git ignores. For your own findings, `ruff check --fix` and `ruff format` handle import order and layout. A catch-all `except Exception` that keeps a worker alive (ap2, wk2) passes once it logs with `log.exception(...)` or `exc_info=True`; if it truly must stay silent, say why with `# noqa: BLE001  # reason`. Don't switch rules off in `[tool.ruff.lint]` just to go green: the reviewer asks for ruff clean. Run the pipeline's commands yourself in a fresh clone before pushing: a file you forgot to commit only fails there. Keep `rules:` that limit jobs to `main` off the lint and test jobs.

</details>
