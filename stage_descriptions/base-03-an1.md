In this stage, you'll run the detector on a video and report when each label appears.

The course ships a detector, `fwdetect`, as a black box: treat it like a model your researchers handed you. You call it on frames and build the service around it. Read its contract with `uv run python -c "import fwdetect; help(fwdetect)"`. Don't read or change its code.

### Expected behaviour

```
$ ./your_program.sh analyze /tmp/street-apple.avi --every 5 --max-gap 0.5
{"labels": {"car": [{"start_s": 4.0, "end_s": 11.0}], "person": [{"start_s": 1.0, "end_s": 8.0}]}}
```

- Run `fwdetect.detect` on every `--every`-th frame (default 1): frames 0, N, 2N, and so on. Call it exactly once per sampled frame, and never on the others.
- A frame's time is its index divided by the fps.
- For each label seen, report segments from the first sighting to the last. Start a new segment when the gap between two sightings is longer than `--max-gap` seconds (default 0.5).
- Leave out labels that were never seen.

### Tests

The tester analyzes a 12-second street clip (a person walks right, a car drives left) with the defaults, with `--every 5`, and a noisy version with `--every 2 --max-gap 0.3`. Each segment boundary may be off by one frame. With `FWDETECT_TRACE` set, the detector logs every call, so the tester also checks that it was called exactly once per sampled frame.

### Pointers

- Keep turning sightings into segments separate from reading the video: it's a plain function of `(time, labels)` pairs that you can test without a video.
- Search: `python dataclass`, `itertools groupby`.
