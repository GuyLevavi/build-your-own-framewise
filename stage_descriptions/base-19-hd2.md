In this stage, you'll limit what can be uploaded.

### Expected behaviour

- The upload limit comes from the environment variable `FRAMEWISE_MAX_UPLOAD_MB` (default 100), in MiB.
- A file whose extension is not `.avi`, `.mp4`, `.mov` or `.mkv` (in any letter case) gives `415`.
- A file larger than the limit gives `413`. A file of exactly the limit is accepted.
- Both errors are JSON with a `detail` field, and nothing is stored.

### Tests

With `FRAMEWISE_MAX_UPLOAD_MB=1`, the tester uploads:

| File | Expected |
| --- | --- |
| a `.txt` file | `415` |
| a video with no extension | `415` |
| exactly 1 MiB, `.mp4` | `201` |
| 1 MiB + 1 byte, `.mp4` | `413` |
| 5 MiB, `.mkv` | `413` |
| a clip named `*.AVI` | `201` |

Then it runs a normal job.

### Notes

- Don't trust `Content-Length` alone: count the bytes you actually write, and delete the partial file when you stop.
- Rejecting a huge upload early (from `Content-Length`, before reading the body) saves the server's disk and time; just leave room for the multipart headers around the file.
