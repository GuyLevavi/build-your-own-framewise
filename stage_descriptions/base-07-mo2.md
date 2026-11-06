In this stage, you'll report separate bursts of motion as separate segments.

### Expected behaviour

A clip where the square moves during frames 5–12, stands still, then moves again during frames 25–34:

```
$ ./your_program.sh analyze /tmp/two_moves-apple.avi
{"segments": [{"start_s": 0.5, "end_s": 1.2}, {"start_s": 2.5, "end_s": 3.4}]}
```

- A segment is a run of consecutive motion frames.
- Segments are in time order.

### Tests

The tester analyzes the two-burst clip and expects exactly two segments, each boundary within one frame. It also re-checks the single-burst clip.

### Notes

- Collect the motion frame indices first, then group them. Keeping detection and grouping apart makes the next stage a one-line change.
