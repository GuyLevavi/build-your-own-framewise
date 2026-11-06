In this stage, you'll add an `analyze` command that reports when the picture changes.

### Expected behaviour

```
$ ./your_program.sh analyze /tmp/square-apple.avi
{"segments": [{"start_s": 1.0, "end_s": 2.9}]}
$ ./your_program.sh analyze /tmp/still-pear.avi
{"segments": []}
```

Definitions for this stage:

- Frame *i* (for *i* ≥ 1) is a **motion frame** when any pixel differs from frame *i − 1*.
- The result is one segment from the first motion frame to the last, or no segment when there is no motion.
- Times are `frame_index / fps`, in seconds.

### Tests

The tester analyzes three clips: a white square that moves during frames 10 to 29 of a black 10 fps clip, a clip that never changes, and a larger 15 fps clip. Each boundary may be off by at most one frame.

### Notes

- The clips are synthetic and compressed one frame at a time, so frames that show the same picture decode to the same pixels. Real video is never this clean; stage 9 deals with that.
- NumPy compares whole frames at once: `np.any(a != b)`.
