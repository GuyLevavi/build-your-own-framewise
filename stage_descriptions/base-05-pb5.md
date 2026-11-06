In this stage, you'll save sample frames as JPEG files.

### Expected behaviour

```
$ ./your_program.sh probe /tmp/square-apple.avi --save-every 10 --out /tmp/frames
{"width": 160, "height": 120, "fps": 10.0, "frame_count": 40, "duration_s": 4.0}
$ ls /tmp/frames
frame_00000.jpg  frame_00010.jpg  frame_00020.jpg  frame_00030.jpg
```

- `--save-every N` saves every frame whose index (counting from 0) is a multiple of N.
- `--out DIR` is where they go. Create it, including missing parent directories.
- `frame_NNNNN.jpg` holds frame NNNNN: five digits, zero-padded.
- The probe JSON is still printed.

### Tests

The tester runs `--save-every` with a random N on two clips, into a directory that does not exist yet. It checks that exactly the expected files exist, that each is a JPEG of the video's size, and that each shows the right frame (it looks for a moving white square at the position it has in that frame).

### Notes

- Read frames one at a time with `VideoCapture.read()` in a loop; never load the whole video into memory. Later stages reuse this loop.
- `cv2.imwrite` picks the format from the file extension.
