In this stage, you'll reject a bad config with a message that names the bad setting.

### Expected behaviour

The same clean-failure contract as stage 3: non-zero exit, one message on stderr, nothing on stdout, no traceback.

| Problem | stderr must contain |
| --- | --- |
| `threshold` not an integer from 0 to 255 | `threshold` |
| `min_area` not an integer ≥ 1 | `min_area` |
| `--detector` names an entry that does not exist | that name |
| `--config` points at a file that does not exist | the file name |

```
$ ./your_program.sh analyze clip.avi --config bad.yaml --detector fast
error: bad.yaml: detectors.fast.threshold must be an integer between 0 and 255, got -5
```

### Tests

The tester runs `analyze` with a negative threshold, a threshold of 300, `min_area: lots`, an unknown detector name and a missing config file, and checks each failure.

### Notes

- Validate when loading, before touching the video, so a typo fails fast.
- `True` is an `int` in Python. Decide whether `threshold: yes` should pass.
