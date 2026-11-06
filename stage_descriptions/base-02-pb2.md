In this stage, you'll add timing to the `probe` output.

### Expected behaviour

```
$ ./your_program.sh probe /tmp/square-apple.avi
{"width": 160, "height": 120, "fps": 10.0, "frame_count": 40, "duration_s": 4.0}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `fps` | number | frames per second |
| `frame_count` | integer | number of frames in the file |
| `duration_s` | number | `frame_count / fps` |

### Tests

The tester probes two clips with different sizes and frame rates (10 and 15 fps). `frame_count` must match exactly; `fps` and `duration_s` may be off by at most 0.01.

### Notes

- `VideoCapture.get` returns floats for every property. Convert counts to `int` so the JSON says `40`, not `40.0`.
