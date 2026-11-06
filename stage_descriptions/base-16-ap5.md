In this stage, you'll move analysis out of the request.

Analysis can take minutes on real video. A request that waits for it ties up the server and times out the client. Record the job, answer at once, and do the work in the background.

### Expected behaviour

- `POST /jobs` answers `202` within 2 seconds with `"status": "PENDING"` (or `RUNNING`), before the analysis is done.
- A background worker runs the jobs; `GET /jobs/{id}` shows the status moving to `DONE` or `FAILED`.
- `GET /health` keeps answering quickly while jobs run.

### Tests

The tester uploads a 10-second 640×360 clip and submits three jobs on it in a row. Each `POST /jobs` must answer within 2 s with `PENDING` or `RUNNING`. While they run, `GET /health` must answer within 1 s. All three must then reach `DONE` with the right segment.

### Notes

- One daemon `threading.Thread` reading job ids from a `queue.Queue` is enough. FastAPI's `BackgroundTasks` works too, but think about what happens to it in the next stage.
- Two threads now touch your job records. Make sure a reader never sees a half-updated job.
