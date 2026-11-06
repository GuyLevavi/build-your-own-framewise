In this stage, you'll stop sensor noise from counting as motion.

Real cameras never produce two identical frames: every pixel flickers a little. Your "any pixel differs" rule now sees motion everywhere.

### Expected behaviour

Change the rule for a motion frame to:

1. Convert both frames to grayscale.
2. A pixel **changed** when the absolute difference is greater than `threshold = 25`.
3. A frame is a motion frame when at least `min_area = 20` pixels changed.

Segments and the 0.5 s gap rule stay as they are.

```
$ ./your_program.sh analyze /tmp/noisy_still-apple.avi
{"segments": []}
$ ./your_program.sh analyze /tmp/noisy_square-pear.avi
{"segments": [{"start_s": 0.8, "end_s": 2.1}]}
```

### Tests

The tester analyzes a still clip with random noise on every pixel (no segments expected), the moving square on that same noise (one segment), and the clean moving square again.

### Notes

- Subtracting two `uint8` arrays wraps around (`3 - 5 == 254`). Use `cv2.absdiff`, or convert to a signed type first.
- Count changed pixels with `np.count_nonzero(diff > threshold)`.
