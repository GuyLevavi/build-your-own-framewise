In this stage, you'll make `probe` print a video's frame size.

Your program is a command-line tool. Its first subcommand, `probe`, opens a video file and prints facts about it as one JSON object on stdout.

### Expected behaviour

```
$ ./your_program.sh probe /tmp/square-apple.avi
{"width": 160, "height": 120}
```

- `width` and `height` are integers, in pixels.
- stdout holds the JSON object and nothing else. Send any debug output to stderr.

### Tests

The tester copies two small MJPG `.avi` clips (160×120 and 240×180) to random file names and runs:

```
$ ./your_program.sh probe <path>
```

It expects exit code 0 and the right `width` and `height` for each clip.

### Notes

- The starter code in `app/main.py` passes this stage once you uncomment it. Read it: the
  `cv2.VideoCapture` it opens is the object every later stage builds on.
- OpenCV's [Getting Started with Videos](https://docs.opencv.org/4.x/dd/d43/tutorial_py_video_display.html)
  tutorial shows `VideoCapture` and its `CAP_PROP_*` properties.
