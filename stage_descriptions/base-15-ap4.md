In this stage, you'll answer bad requests with the right status codes, and fail jobs cleanly.

### Expected behaviour

| Request | Status |
| --- | --- |
| `GET /jobs/{id}` for an unknown id | `404` |
| `POST /jobs` for an unknown `video_id` | `404` |
| `POST /jobs` without `video_id` | `422` |
| `POST /jobs` with an unknown `detector` | `422` |

Every error body is JSON with a `detail` field.

A job whose video is not playable is not a server error: it ends with `"status": "FAILED"` and a non-empty `error` string, and the server keeps working.

### Tests

The tester makes each bad request above. Then it uploads a file that is not a video (the upload itself succeeds with `201`), runs a job on it and expects `FAILED` with an `error`. Finally it runs a normal job to check the server survived.

### Notes

- A `500` means your code crashed. Turn the errors you expect (unknown id, unreadable video) into deliberate responses.
- FastAPI already returns `422` for a body that doesn't match a Pydantic model.
