In this stage, you'll merge bursts of motion separated by a short pause.

Someone who stops walking for a moment is still one event. Split segments only on a real pause.

### Expected behaviour

Two motion frames belong to the same segment when they are at most **0.5 seconds** apart (`(j - i) / fps <= 0.5`). A clip with motion on frames 5–14, a pause, then motion on frames 18–27 (10 fps, so 0.4 s apart):

```
$ ./your_program.sh analyze /tmp/pause-apple.avi
{"segments": [{"start_s": 0.5, "end_s": 2.7}]}
```

The two-burst clip from the previous stage (1.3 s apart) still gives two segments.

### Tests

The tester analyzes the pause clip (one segment expected) and the two-burst clip (two segments expected), each boundary within one frame.

### Notes

- Keep the 0.5 s in one named constant: stage 10 moves it to a config file.
