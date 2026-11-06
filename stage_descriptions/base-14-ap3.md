In this stage, you'll run motion analysis as a job.

### Expected behaviour

`POST /jobs` takes JSON and answers `202` with the job:

```
$ curl -X POST localhost:8000/jobs -H 'Content-Type: application/json' \
       -d '{"video_id": "4f1c...", "detector": "default"}'
{"id": "9a0e...", "video_id": "4f1c...", "detector": "default", "status": "DONE", "error": null,
 "segments": [{"start_s": 1.0, "end_s": 2.9}]}
```

`GET /jobs/{id}` returns the same object.

| Field | Meaning |
| --- | --- |
| `id` | non-empty string |
| `status` | `PENDING`, `RUNNING`, `DONE` or `FAILED` |
| `segments` | as in `analyze`, once `DONE` |
| `error` | why it failed, once `FAILED` |

`detector` is optional and defaults to `default`. The server uses the same built-in detector as `analyze` without `--config`.

### Tests

The tester uploads a clip, posts a job, and polls `GET /jobs/{id}` until the status is `DONE` or `FAILED` (up to 30 s). It expects `DONE` with the right segments. It repeats this for the two-burst clip with `"detector": "default"` given explicitly.

### Notes

- Running the analysis inside the request is fine for now. Stage 16 changes that, and the tester already polls, so a client written against this contract will keep working.
- Reuse the `analyze` code; the server should not have its own copy of the motion logic.
