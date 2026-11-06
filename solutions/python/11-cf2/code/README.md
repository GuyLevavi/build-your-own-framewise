This is a starting point for Python solutions to the "Build your own Framewise"
challenge, a local course in the CodeCrafters format.

Framewise is a small video analytics service. You'll build it in 19 stages: a
command-line tool that probes videos and finds motion in them, then an HTTP API
that runs analysis jobs in the background, survives restarts and checks an API
key.

# Passing the first stage

The entry point is `app/main.py`. Study and uncomment the code below the
"Uncomment the code below" marker, then run the tests for stage 1 (from the
course repo):

```sh
tools/run_tests.sh path/to/this/repo pb1
```

The first run builds a container image with your dependencies, so it takes a
minute or two. Later runs are quick.

# Stage 2 and beyond

1. Read the next stage in the course repo's `stage_descriptions/` (in order:
   `base-02-pb2.md`, ...).
2. Change `app/main.py` (add modules under `app/` as you like).
3. Run the tests up to that stage:

   ```sh
   tools/run_tests.sh path/to/this/repo pb2
   ```

   This runs every stage up to and including `pb2`, so a change can't silently
   break an earlier stage. Add `--only` to run one stage.

To run your program by hand, use `./your_program.sh` with the same arguments the
tests use, for example `./your_program.sh probe some.avi`. It needs `uv`; on
NixOS, run it in the container instead (see the course README).
