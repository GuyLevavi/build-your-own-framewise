# framewise-tester

Tester for "Build your own Framewise" (course content in
[build-your-own-framewise](https://github.com/GuyLevavi/build-your-own-framewise)). Built on
CodeCrafters' [tester-utils](https://github.com/codecrafters-io/tester-utils), in the
layout of [tester-template](https://github.com/codecrafters-io/tester-template).

## Using it

Like every CodeCrafters tester, it takes no arguments and reads its run from
`CODECRAFTERS_REPOSITORY_DIR`, `CODECRAFTERS_TEST_CASES_JSON` and
`CODECRAFTERS_SKIP_ANTI_CHEAT` (`CODECRAFTERS_RANDOM_SEED` repeats a run). It runs
`./your_program.sh` from that directory.

Learners don't set these by hand: their solution repo
([codecrafters-framewise-python](https://github.com/GuyLevavi/codecrafters-framewise-python))
pins this repo as a flake input, builds it, and its `./test.sh` fills them in. The course
repo's `tools/run_tests.sh` runs it in the course's Python image with podman instead.

```sh
make build              # dist/tester and dist/test.sh (what course-sdk expects)
make test               # stages match the course definition; clips match the stage texts
make copy_course_file   # refresh internal/test_helpers/course_definition.yml
```

## How it tests

- **Clips** (`internal/clips.go`, `internal/avi.go`): every video is synthetic, generated at
  run time as an MJPG `.avi` in pure Go: a white square on black (or on grey noise) that
  moves on known frames. So the motion frames, the segments and the square's position in
  every frame are known exactly, and the tester ships no binary fixtures. Each frame is a
  JPEG of its own, so frames that show the same picture decode to the same pixels.
  `go test` checks that the clips give the numbers the stage descriptions quote.
- **CLI stages** (`stages_probe.go`, `stages_motion.go`): `probe` and `analyze` are run on
  clips with random names; stdout must be exactly one JSON object; failures must be clean
  (non-zero exit, the file or setting named on stderr, no traceback, empty stdout).
  Segment boundaries may be off by one frame. Saved frames are decoded and the square is
  located in them.
- **Server stages** (`server.go`, `stages_api.go`): `serve` is started on a free port with a
  fresh `--data-dir` and polled on `GET /health` for 30 s. Error bodies must be JSON with
  `detail`. Stage ap5 times `POST /jobs` (< 2 s, still `PENDING`/`RUNNING`) and `GET /health`
  (< 1 s) while a 10 s 640×360 clip is analyzed three times; ap6 stops the server with
  SIGTERM and starts it again on the same `--data-dir`.
- **Model server** (`modelserver.go`, `stages_model.go`): from md1 the tester runs a stand-in for
  Triton on a free port and passes `FRAMEWISE_MODEL_URL`. It speaks the KServe v2 HTTP protocol
  (JSON or binary tensors), publishes its input size, `max_batch_size` and `instance_group`, and
  finds fwdetect's coloured boxes in RGB frames. Each request costs a fixed latency plus a cost
  per frame, so md2's 160 frames/s target doesn't depend on the machine. It can answer 503,
  hang, refuse connections, or fail with 500 on a grey frame (md3).
