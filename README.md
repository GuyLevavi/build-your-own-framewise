# Build your own Framewise — Python (local)

A solution to the [Build your own Framewise](https://github.com/GuyLevavi/build-your-own-framewise) challenge,
tested locally against its tester.
You don't need a CodeCrafters account.

The repo is self-contained. The course repo
([build-your-own-framewise](https://github.com/GuyLevavi/build-your-own-framewise))
and the [framewise-tester](https://github.com/GuyLevavi/framewise-tester) are flake inputs, pinned in
`flake.lock`. The tester is built from source as `.#framewise-tester`.

```sh
git clone https://github.com/GuyLevavi/codecrafters-framewise-python && cd codecrafters-framewise-python
direnv allow         # or: nix develop
./test.sh
```

## Dev shell

`nix develop`, or direnv via `.envrc`, provides Python 3 with every dependency in `pyproject.toml`
(including the course's `fwdetect`, built from the course input), ruff, py-spy, `psql`, helm and the tester.
`test.sh` enters the dev shell by itself if needed.

`nix flake update course tester` pulls newer upstream stages and tester fixes.

## Stage instructions

```sh
./test.sh --show          # current stage
./test.sh --show pb2      # any stage
```

The files live in `$FRAMEWISE_COURSE_DIR/stage_descriptions/`. Stage order and
names come from `$FRAMEWISE_COURSE_DIR/course-definition.yml`.

## The delivery stages

ci1 to hm2 build images, run a pod and render a Helm chart. helm comes from the dev shell; podman
must be the system's (rootless podman needs setuid helpers nix can't install).

## Postgres

From stage ap1 the tester needs a Postgres server it may create databases on: set
`FRAMEWISE_TEST_DATABASE_URL` (for example the team's hosted one), or have podman installed and the
tester starts a throwaway `postgres:18` itself.

## Fast track (reviewers)

```sh
./review.sh wk3          # app/ becomes the reference solution of wk2, .stage becomes wk3,
                         # and the brief for wk3 opens
./test.sh                # what the learner sees on wk3; do it yourself, or sketch it
./review.sh --diff wk3   # then compare with the reference
./review.sh --brief      # the current stage's brief again (--full: whole assignment weeks)
./review.sh --list       # every stage with its Framewise weeks; ★ = worth doing yourself
```

For reviewers learning the course quickly: do one stage on top of the reference solution of the
one before, with the same `./test.sh` a learner uses. It overwrites `app/` (and, for the delivery
stages, `tests/`, `.gitlab-ci.yml`, `Containerfile`, `.containerignore` and `deploy/`), so commit first.

The brief is one page per stage: the task and why the design breaks, the files the reference
changed and a map of the code so far, what to look for in review, and the Framewise weeks the stage
stands for (what each adds, its task, when it's done, its reading list, the tutor's checks). That
last part reads `ASSIGNMENTS.md` and `TUTOR.md` from a
[framewise](https://github.com/GuyLevavi/framewise) clone next to this repo, or `FRAMEWISE_REPO`.
Both scripts come from the pinned course (`tools/fast_track.sh`, `tools/brief.py`); its
`REVIEWING.md` has the full review notes.

## Tests

```sh
./test.sh            # every stage up to the current one (stored in .stage)
./test.sh <slug>     # one stage, e.g. ./test.sh pb2
./test.sh --list     # all stages in order, * marks the current one
./test.sh --next     # move .stage forward after passing
```

The tester stops at the first failing stage. It runs `./your_program.sh`, which
builds the program and then runs it.

## Local deviations from the starter

- `test.sh`, `.stage`, `flake.nix`, `flake.lock` and `.envrc` are local additions, and so is
  `review.sh`, the reviewer's fast track.
- `your_program.sh` runs `python -m app.main` instead of `uv run`: the devShell's Python
  comes from nixpkgs with the dependencies in `pyproject.toml`, because the
  `opencv-python-headless` wheel that uv would install doesn't run on NixOS. Versions follow
  nixpkgs, not `uv.lock`. Scalene isn't in the dev shell; use py-spy here, or `uv run --group profile`
  outside NixOS.

