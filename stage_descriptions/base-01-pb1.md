In this stage, you'll make `probe` print the facts of a video file.

Your program is a command-line tool first and a service later. Its first subcommand, `probe`, opens a video and prints one JSON object on stdout. Everything the service does later starts with opening a file like this.

### New here?

- **Frame**: one still picture of a video. A video is frames shown one after another.
- **fps** (frames per second): how many frames make one second. Frame *i* is shown at *i / fps* seconds.
- **OpenCV** (`cv2`): the library that opens videos and hands you frames as NumPy arrays.

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

### In review

The tester can't check this part, so a reviewer will: you'll be asked to show a real command-line interface (argparse or typer), JSON on stdout and nothing else there, and no work done at import time. From here on every stage's review also asks for type hints that `basedpyright` (the team's checker) accepts, `ruff` clean, unit tests for pure logic, and a README section for each command.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

`cv2.VideoCapture(path)` opens the file; `capture.get(cv2.CAP_PROP_...)` reads its facts. The starter already reads the width and height this way.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

One subcommand per action: `argparse` with `add_subparsers`, a `probe` parser taking a `path`, and a function that returns a dict you `json.dumps` once. Keep the facts in a small dataclass with an `as_dict()`: later stages reuse it.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`CAP_PROP_FRAME_COUNT` and `CAP_PROP_FPS` come back as floats: cast the counts to `int`. Anything else you print (a debug line) breaks the JSON on stdout; print it to stderr.

</details>
