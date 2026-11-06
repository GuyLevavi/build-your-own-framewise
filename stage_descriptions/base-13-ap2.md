In this stage, you'll accept video uploads.

### Expected behaviour

`POST /videos` takes a multipart form with one field, `file`:

```
$ curl -F file=@square.avi localhost:8000/videos
{"video_id": "4f1c...", "filename": "square.avi", "size_bytes": 29390}      (201)
```

`GET /videos/{video_id}` describes an upload:

```
$ curl localhost:8000/videos/4f1c...
{"video_id": "4f1c...", "filename": "square.avi", "size_bytes": 29390}      (200)
```

- `video_id` is a non-empty string, different for every upload.
- `size_bytes` is the exact number of bytes received.
- An unknown id gives `404` with a JSON body that has a `detail` field.

### Tests

The tester uploads two clips, checks their ids differ, reads each back and compares `filename` and `size_bytes`. Then it requests a random id and expects `404`.

### Notes

- Store each upload under `--data-dir`, named by its id, not by the client's file name: two clients may both send `video.mp4`, and a file name can contain `../`.
- FastAPI's `UploadFile` gives you a file object to copy from; `shutil.copyfileobj` streams it.
