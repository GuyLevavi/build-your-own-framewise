In this stage, you'll make `probe` print the facts of a video file.

Your program is a command-line tool first and a service later. Its first subcommand, `probe`, opens a video and prints one JSON object on stdout. Everything the service does later starts with opening a file like this.

### Expected behaviour

```
$ ./your_program.sh probe /tmp/street-apple.avi
{"width": 640, "height": 360, "fps": 10.0, "frame_count": 120, "duration_s": 12.0}
```

- `width`, `height` and `frame_count` are integers; `fps` and `duration_s` are numbers.
- `duration_s` is `frame_count / fps`.
- stdout holds the JSON object and nothing else. Send any debug output to stderr.

### Tests

The tester writes two synthetic MJPG clips (640×360 at 10 fps: 120 frames and 60 frames) to random file names, runs `./your_program.sh probe <path>` on each, and checks every field (`fps` and `duration_s` within 0.01).

### Pointers

- The starter code in `app/main.py` prints the size once you uncomment it; the rest is in `VideoCapture`'s `CAP_PROP_*` properties.
- [Getting Started with Videos](https://docs.opencv.org/4.x/dd/d43/tutorial_py_video_display.html) (OpenCV). Search: `cv2.CAP_PROP_FRAME_COUNT`, `argparse subparsers`.

Prepares for Framewise week 1.
