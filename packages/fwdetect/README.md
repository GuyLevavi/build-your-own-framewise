# fwdetect

The object detector the course ships as a black box. Learners install the wheel and
call `fwdetect.detect(frame)`; they never read or change this code. The contract they
may rely on is the docstring in `src/fwdetect/__init__.py`.

It finds the coloured rectangles that the tester (`tester/`) draws in its scenes (person red,
car blue, bicycle green, drawn additively so overlaps keep whole boxes). Its cost
stands in for a real model: work proportional to the pixel count, mostly holding the
GIL, so the performance stages have something real to optimize. A uniform mid-grey
frame aborts the process, which is what the crashed-worker stage relies on.

## Build and test

```sh
uv build --wheel -o dist .            # dist/fwdetect-<version>-py3-none-any.whl, for the release
uv run --with pytest pytest tests     # the contract, on frames built in NumPy

# The same, plus the tester's own clips decoded by OpenCV:
(cd ../../tester && FRAMEWISE_DUMP_SCENES=/tmp/scenes go test -run TestDumpScenes ./internal)
FWDETECT_SCENES=/tmp/scenes uv run --with pytest --with opencv-python-headless pytest tests
```

`python -m fwdetect bench` prints single-core frames per second at 640x360. The tester
runs it to calibrate the fps targets of the performance stages.
