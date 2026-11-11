This is a starting point for {{language_name}} solutions to the "Build your own Framewise"
challenge, a local course in the CodeCrafters format.

Framewise is a small video analytics service. You'll build it in 30 stages: a command-line tool
that probes videos and finds motion in them, then an API with a Postgres job queue and workers,
results in SQL, multi-process performance work, a model server, metrics, and last a pipeline, an
image, a pod and a Helm chart that ship it.

# Running the tests

In the course repo this directory is `attempts/<language>/`. From the repo's root:

```sh
./test.sh                # every stage up to the one in .stage (Nix builds the tester and the toolchain)
./test.sh pb2            # one stage
./test.sh --show         # the current stage's instructions
./test.sh --next         # move on, once it passes
```

Without Nix, from the course repo, against any copy of this directory:

```sh
tools/run_tests.sh path/to/this/dir pb2           # stages 1..pb2, the program in a container
FRAMEWISE_RUNNER=native tools/run_tests.sh path/to/this/dir pb2   # with uv on this machine
```

Every run re-runs the earlier stages too, so a change can't silently break one. With
`tools/run_tests.sh`, add `--only` to run one stage. The first container run builds an image with
your dependencies, so it takes a minute or two.

# Passing the first stage

The entry point is `{{ user_editable_file }}`. Study and uncomment the code below the
"Uncomment the code below" marker, then run the tests for stage 1.

# Stage 2 and beyond

1. Read the next stage: `./test.sh --show`, or `stage_descriptions/` in the course repo.
2. Change `{{ user_editable_file }}` (add modules under `app/` as you like).
3. Run the tests.

To run your program by hand, use `./your_program.sh` with the same arguments the tests use, for
example `./your_program.sh probe some.avi`. It uses `uv`, or the Nix dev shell's Python inside
`nix develop .#python`.
