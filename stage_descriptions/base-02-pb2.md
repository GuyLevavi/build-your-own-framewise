In this stage, you'll make bad input fail with one clear line instead of a traceback.

A traceback tells the user your program crashed. A message that names the file tells them what to fix. From now on, any file or setting the user got wrong must fail this way.

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

Prepares for Framewise week 1.
