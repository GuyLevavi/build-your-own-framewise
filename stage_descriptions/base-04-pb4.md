In this stage, you'll reject files that exist but are not playable video.

### Expected behaviour

Same contract as the previous stage, for a file that exists:

```
$ ./your_program.sh probe /tmp/garbage-pear.mp4
error: /tmp/garbage-pear.mp4: not a readable video
$ echo $?
1
```

### Tests

The tester writes three bad files with random names and probes each:

- `garbage-*.mp4`: plain text, not video at all
- `empty-*.avi`: zero bytes
- `truncated-*.avi`: the first 300 bytes of a real clip, so the header is there but no frame is

For each it expects a non-zero exit code, the file name in stderr, nothing on stdout and no traceback.

### Notes

- `VideoCapture.isOpened()` catches the first two. The truncated file may open fine; only reading a frame shows it has none.
- OpenCV may print its own warnings to stderr. That's allowed, as long as your message names the file.
