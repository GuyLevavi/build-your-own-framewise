In this stage, you'll make 1080p analysis several times faster by running the detector at a smaller size.

Cameras send 1080p and the detector's cost grows with every pixel. `fwdetect`'s contract says its accuracy is the same at any width from 320 up, so most of that work buys nothing. This is the first stage measured in frames per second, which is how the team judges the real service.

### Expected behaviour

- A job on a 1920x1080 video runs at least 0.35 times as many frames per second as a job on a 640x360 one, on the same machine.
- Labels and stored detections are unchanged: boxes are in the coordinates of the original video, whatever size the detector saw.

### Tests

The tester sets `FWDETECT_TRACE` and measures detector calls per second from the trace's timestamps, so start-up time doesn't count. It runs a job on the 640x360 street clip to calibrate this machine, then a job on a 20-second 1080p clip, and expects at least 0.35 of the calibrated rate. It checks the labels and the detections between 8 and 10 s (positions within 6 px of the 1080p truth).

Run `python -m fwdetect bench` to see the detector's speed on your machine.

### Why your design just broke

Nothing in your code knew a frame's size, and now one function does: the one that calls the detector. Keep it the only one. A thin wrapper that resizes, detects and scales boxes back is all it takes, if every caller goes through it. Measure before you choose how to resize: `cv2.resize` has several interpolations with very different costs, and decoding itself costs something you can't avoid.

### Pointers

- [Scalene](https://github.com/plasma-umass/scalene) (in the `profile` dependency group: `uv sync --group profile`) shows time and memory per line, split into Python and native code. [py-spy](https://github.com/benfred/py-spy) samples a process that is already running: `py-spy top --pid <worker>` or `py-spy record -o profile.svg -- python -m app.main analyze ...`.
- [`cv2.resize`](https://docs.opencv.org/4.x/da/d54/group__imgproc__transform.html#ga47a974309e9102f5f08231edc7e7529d) and its interpolation flags.
- Search: `INTER_AREA vs INTER_LINEAR downscale`, `frames per second budget video pipeline`.
