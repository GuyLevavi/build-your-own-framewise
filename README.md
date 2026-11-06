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

`nix develop`, or direnv via `.envrc`, provides Python 3 with OpenCV, NumPy, FastAPI, Uvicorn and PyYAML,
and the tester.
`test.sh` enters the dev shell by itself if needed.

`nix flake update course tester` pulls newer upstream stages and tester fixes.

## Stage instructions

```sh
./test.sh --show          # current stage
./test.sh --show pb2      # any stage
```

The files live in `$FRAMEWISE_COURSE_DIR/stage_descriptions/`. Stage order and
names come from `$FRAMEWISE_COURSE_DIR/course-definition.yml`.

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

- `test.sh`, `.stage`, `flake.nix`, `flake.lock` and `.envrc` are local additions.
- `your_program.sh` runs `python -m app.main` instead of `uv run`: the devShell's Python
  comes from nixpkgs with the dependencies in `pyproject.toml` (OpenCV, NumPy, FastAPI,
  Uvicorn, PyYAML, python-multipart), because the `opencv-python-headless` wheel that uv
  would install doesn't run on NixOS. Versions follow nixpkgs, not `uv.lock`.

