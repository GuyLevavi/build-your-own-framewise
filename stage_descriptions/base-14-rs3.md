In this stage, you'll make a retried job store its results exactly once, and keep each video's events as episodes that a rerun doesn't duplicate.

Leases and batched writes meet here. A worker that dies halfway through a job has already committed some batches of detections. The retry writes them all again, and every count, range and event built on that table is now wrong.

Reruns are the same problem from the other side. Computing events on every request is fine for one job, but a dashboard over every video wants them as rows, and videos get analyzed again: a new profile, a fixed bug, a curious user. Each rerun must not add another copy of the same episode.

### New here?

- **Idempotent**: doing it twice has the same effect as doing it once. Retries are only safe for idempotent writes.
- **At-least-once**: a job may run more than once (a retry after a crash), so its writes must not pile up.
- **Upsert**: `INSERT ... ON CONFLICT (key) DO UPDATE`: insert a row, or update the one that's already there.

### Expected behaviour

- After a job is retried, `results.detections` holds exactly one set of rows for it: what one complete analysis produces.
- When a job finishes `DONE`, workers store its events (rs2's rules, from `FRAMEWISE_EVENTS`, which workers now read too) in a table `results.episodes` with at least `video_id`, `rule`, `start_s` and `end_s`.
- `GET /videos/{video_id}/events` answers `{"events": [{"rule", "start_s", "end_s"}, ...]}` from `results.episodes`, sorted by `start_s` then `rule`. An unknown video is 404.
- Running the same video again leaves one row per episode, not two.
- Everything from earlier stages still holds: detections and events are right after a retry.

### Tests

The tester runs one supervised worker with `FRAMEWISE_LEASE_S=3`, submits the long clip, waits until more than 100 rows are stored for the job, and kills the worker with SIGKILL. When the job is `DONE`, its row count must be exactly that of one full run, and its events must be right. Then it runs the street clip twice and, after each run, checks `GET /videos/{id}/events` and that `results.episodes` holds exactly one row per event for that video. It checks 404 for an unknown video.

### Why your design just broke

Retries are only safe if what a job writes is idempotent. Several fixes work, with different costs: delete the job's rows when an attempt starts (in the same transaction as the claim), tag rows with the attempt and read only the latest, or make rows unique on (job, frame, label) and upsert. Pick one and be ready to say why in review. Whatever you pick, the claim and the cleanup must not be able to interleave with another worker.

Episodes need a key that comes from what the episode is, not from when it was written: a hash of the video, the rule and the start time. A rerun computes the same key, and `INSERT ... ON CONFLICT` turns the second write into an update. A serial id can't do that, because every insert gets a new one.

### Pointers

- [`INSERT ... ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT).
- Search: `idempotent consumer`, `at-least-once delivery`, `exactly-once is at-least-once plus idempotence`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to say why you chose your way of removing a failed attempt's rows. Episodes are expected to be written in the same transaction that marks the job DONE, so a job is never DONE without them.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Two fixes. Detections: when a worker claims a job, delete whatever an earlier attempt of it stored, in the same transaction as the claim. Episodes: give each one a key computed from what it is (video, rule, start time), so a rerun writes the same keys and updates instead of adding.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

In `claim()`, right after the `UPDATE ... SET status = 'RUNNING'`: `DELETE FROM results.detections WHERE job_id = :id`. Migration `0004_episodes.py` creates `results.episodes (key text primary key, video_id, rule, start_s, end_s)`. When a job finishes DONE, run rs2's query for each rule and `insert(...).on_conflict_do_update(index_elements=["key"], set_=...)` (from `sqlalchemy.dialects.postgresql`), with `key = sha256(f"{video_id}:{rule}:{start_s:.3f}")`.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

A cleanup in a separate transaction can interleave with another worker: it must be in the claim's. Round the start time before hashing it, or 4.800000001 and 4.8 make two episodes.

</details>
