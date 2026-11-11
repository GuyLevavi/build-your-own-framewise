In this stage, you'll move job execution out of the API into its own `work` process.

Your server runs jobs on a thread inside itself. That ties two things together that scale and fail differently: the API should answer in milliseconds and restart freely, while analysis is slow, CPU-bound and wants more machines. From now on `serve` only records jobs, and workers run them.

### Expected behaviour

- `./your_program.sh work` runs until it is stopped. It reads `DATABASE_URL` and `FRAMEWISE_PROFILES` like `serve`, takes `PENDING` jobs from `queue.jobs`, marks each `RUNNING`, analyzes it and records `DONE` or `FAILED`, as the in-process worker did.
- `serve` no longer runs jobs. With no worker running, a submitted job stays `PENDING`.
- The two talk only through Postgres. A worker keeps going when the server dies, and a new server sees what the worker recorded.

### Tests

The tester starts `serve` with no worker, submits a job on the street clip and checks it is still `PENDING` 3 seconds later. It starts one `work` process and waits for the job to reach `DONE` with the right labels. It then submits a job on the long clip, waits for `RUNNING`, kills the server with SIGKILL, and checks the worker is still alive. It starts a new server and waits for that job to reach `DONE`.

Earlier stages now start a `work` process next to `serve` too, so they keep passing.

### Why your design just broke

The thread, its in-memory queue and the "requeue unfinished jobs at startup" step all go. The database is the queue now: a worker finds work with a query, and taking a job must be atomic, a `SELECT ... FOR UPDATE` and an `UPDATE` in one transaction. If the repository from the last stage owns all SQL, the worker becomes a short loop over `claim()` and `finish()`, and `serve` loses code.

### Pointers

- [PostgreSQL: explicit locking, row-level locks](https://www.postgresql.org/docs/current/explicit-locking.html#LOCKING-ROWS) and [`SELECT ... FOR UPDATE`](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE).
- Search: `postgres as a job queue`, `polling worker loop`, `separate API and worker processes`.
