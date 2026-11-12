In this stage, you'll make bad input fail with one clear line instead of a traceback.

A traceback tells the user your program crashed. A message that names the file tells them what to fix. From now on, any file or setting the user got wrong must fail this way.

### New here?

- **stdout / stderr**: two output streams. Results go to stdout, messages for humans to stderr.
- **Exit code**: the number a program ends with. 0 means success, anything else failure; `$?` in the shell shows it.

### Expected behaviour

For a path that doesn't exist, or a file that isn't a playable video (garbage, empty, or cut off after its header):

```
$ ./your_program.sh probe /tmp/garbage-apple.mp4
error: /tmp/garbage-apple.mp4: not a playable video
$ echo $?
1
```

- The exit code is non-zero.
- stderr names the file. The exact wording is yours.
- stderr has no Python traceback, and stdout is empty.

### Tests

The tester runs `probe` on a missing file, a text file named `.mp4`, an empty `.avi`, and the first 300 bytes of a real clip. A file cut off after its header still opens in OpenCV and even reports a frame count, so check that a frame actually decodes.

### Pointers

- One exception type for "the user's fault", caught once in `main`, is easier to keep consistent than `try` blocks around every call.
- Search: `python custom exception hierarchy`, `cv2.VideoCapture isOpened`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Decide in one place what "the user's fault" looks like: an exception type of your own, raised wherever you detect bad input, and caught once in `main`.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

`class FramewiseError(Exception)`; in `main`: `try: args.run(args)` / `except FramewiseError as e: print(f"error: {e}", file=sys.stderr); sys.exit(1)`. The probe raises it when the file is missing, when `capture.isOpened()` is false, and when the first `capture.read()` returns no frame.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

A truncated file opens fine and reports a frame count; only reading a frame shows it's broken. Don't catch every `Exception` in `main`: a real bug should still show its traceback to you.

</details>
