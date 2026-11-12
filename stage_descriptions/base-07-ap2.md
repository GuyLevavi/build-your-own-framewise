In this stage, you'll run analysis jobs in the background and report their status.

Analysis takes as long as the video does. A request that waits for it ties up the server, and the client times out. Accept the job, answer at once, and do the work elsewhere. For now, "elsewhere" is a thread inside the server. That's deliberately naive, and later stages will take it apart.

### New here?

- **Background job**: work that is accepted now and done later. The client gets an id at once (`202 Accepted`) and asks about it afterwards.
- **Thread**: a second line of execution inside the same process. Threads share memory, so two of them can see each other's half-finished changes.
- **Queue** (`queue.Queue`): a thread-safe line of items; one thread puts, another takes.

### Expected behaviour

The server reads its profiles from the YAML file named by `FRAMEWISE_PROFILES` (an3's format). `default` is not built in: it's a profile in that file like any other, and the tester's file defines it (`every: 1`, `max_gap_s: 0.5`, `person`, `car` and `bicycle`, the same as `analyze` without `--profile`).

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

### In review

The tester can't check this part, so a reviewer will: you'll be asked to say which part of the code owns the background thread, and show that analysis runs off the event loop.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

The request handler only records the job and puts its id on a queue. One worker thread, started with the server, takes ids off the queue and runs the analysis you already have.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

A `Job` record (id, video_id, profile, status, error, labels) in a dict, guarded by a `threading.Lock`; a daemon `threading.Thread(target=worker_loop, daemon=True)`; `worker_loop` does `job_id = q.get()`, sets RUNNING, calls `analyze`, then sets DONE with labels, or FAILED with `str(e)` when it raises.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

If the worker thread raises and nothing catches it, the thread dies and every later job stays PENDING forever: catch exceptions per job inside the loop, and log them with `log.exception(...)` so the traceback isn't lost. A bare `except Exception: pass` hides bugs, and the linter you add in ci1 rejects a catch-all that neither logs the traceback nor re-raises. Validate the profile name in the handler (422), not in the worker.

</details>
