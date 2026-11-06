In this stage, you'll make a missing file fail cleanly.

A command-line tool that crashes with a Python traceback makes its user read your code to find out what they did wrong. Tell them instead.

### Expected behaviour

```
$ ./your_program.sh probe /tmp/nope.mp4
error: /tmp/nope.mp4: no such file
$ echo $?
1
```

- Exit with a non-zero code.
- Write one message to stderr that contains the file name.
- Write nothing to stdout.
- No `Traceback` anywhere in stderr.

### Tests

The tester probes two paths that do not exist (one `.mp4`, one `.avi`) and checks all four points above.

### Notes

- OpenCV does **not** raise on a missing file: `VideoCapture` just reports zeros. Check the path yourself.
- `pathlib.Path.is_file()` and a custom exception caught once in `main()` keep the error handling in one place.
