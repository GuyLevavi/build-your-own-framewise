In this stage, you'll run analysis jobs in the background and report their status.

Analysis takes as long as the video does. A request that waits for it ties up the server, and the client times out. Accept the job, answer at once, and do the work elsewhere. For now, "elsewhere" is a thread inside the server. That's deliberately naive, and later stages will take it apart.

### Expected behaviour

The server reads its profiles from the YAML file named by `FRAMEWISE_PROFILES` (stage 5's format).

- `POST /jobs {"video_id": "...", "profile": "people"}` answers `202 {"id": "...", "status": "PENDING", "video_id": "...", "profile": "people"}` within 2 s, before the analysis is done. `profile` defaults to `default`.
- `GET /jobs/{id}` answers `200 {"id", "video_id", "profile", "status", "error", "labels"}`. `status` moves from `PENDING` to `RUNNING`, then to `DONE` or `FAILED`. When `DONE`, `labels` holds what `analyze` would print for that profile. When `FAILED`, `error` says why.
- `404` for an unknown job or video, and `422` for a missing `video_id` or an unknown profile.
- A job on a file that isn't a video ends `FAILED` with a non-empty `error`. The server keeps running.
- `GET /health` keeps answering within 1 s while jobs run.

### Tests

The tester runs a job with the default profile and one with `people` on the street clip, then tries each error case and a job on garbage. Finally it submits three jobs on the 10-minute clip: each `POST` must answer within 2 s, and `/health` within 1 s while they run. All three must reach `DONE`.

### Pointers

- One daemon `threading.Thread` reading job ids from a `queue.Queue` is enough. Two threads now share your job records, so a reader must never see a half-updated job.
- Search: `python queue.Queue worker thread`, `FastAPI status_code 202`.

Prepares for Framewise week 3.
