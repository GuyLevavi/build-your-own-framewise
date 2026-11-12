In this stage, you'll make analysis faster with a profile that runs the detector at a smaller input size.

The detector's cost grows with its input: at 320 x 320 it does about a quarter of the work it does at 640. `models/README.md` says any multiple of 32 works, and that smaller finds small objects less well: a trade the people who run each camera should make, not one you hard-code. So the input size becomes a profile setting. This is the first stage measured in frames per second, which is how the team judges the real service.

### Expected behaviour

```yaml
profiles:
  fast:
    every: 1
    max_gap_s: 0.5
    labels: [person, car, bicycle]
    input_size: 320   # optional, default 640; a multiple of 32, from 32 to 1280
```

- A profile's `input_size` is the size the detector runs at. An `input_size` that isn't a multiple of 32 is an invalid setting, reported by its path like any other.
- Boxes are stored in the coordinates of the original video, whatever size the detector saw.
- The `fast` profile on 1080p video runs at least 1.5 times as many frames per second as the default.

### Tests

The tester watches onnxruntime inside your worker and measures frames per second from when the model ran, so start-up time doesn't count. It runs a job on a 20-second 1920x1080 clip with the default profile, then one with `fast`. It checks that the `fast` job never ran the model on an input larger than 320, that it made at least 1.5x the default's frames per second, and its labels and its detections between 8 and 10 s (each box must overlap what the model finds at 320 by an IoU of at least 0.85). Then it runs `analyze` with `input_size: 300` and expects a clean failure naming `profiles.fast.input_size`.

### Why your design just broke

Until now the input size was a constant next to the code that letterboxes a frame, and that code was the only thing that knew a frame's size. Keep it that way: the size travels from the profile to the one function that prepares a frame and reads boxes back, and nothing else learns about letterboxes. Then measure: once the model's share shrinks, decoding a 1080p frame and resizing it are a real part of the cost, and `cv2.resize`'s interpolations cost very different amounts.

### Pointers

- [Scalene](https://github.com/plasma-umass/scalene) (in the `profile` dependency group: `uv sync --group profile`) shows time and memory per line, split into Python and native code. [py-spy](https://github.com/benfred/py-spy) samples a process that is already running: `py-spy top --pid <worker>` or `py-spy record -o profile.svg -- python -m app.main analyze ...`.
- [`cv2.resize`](https://docs.opencv.org/4.x/da/d54/group__imgproc__transform.html#ga47a974309e9102f5f08231edc7e7529d) and its interpolation flags.
- Search: `pydantic Field multiple_of`, `INTER_AREA vs INTER_LINEAR downscale`.
