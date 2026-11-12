In this stage, you'll run several workers against one queue without two of them taking the same job.

One worker analyzes one job at a time. To go faster you start more, and now they compete for the same rows.

### Expected behaviour

- Any number of `work` processes can run at once. Each job is analyzed by exactly one of them.
- A worker never waits for a job another worker is in the middle of claiming: it takes the next free one.

### Tests

The tester watches onnxruntime inside your workers and logs every frame the detector runs on, with its process id. It submits 12 jobs on the street clip, then locks the first job's row in its own transaction, as a worker in the middle of claiming it would. It starts 3 workers and expects the other 11 jobs to finish while the lock is held. It then releases the lock, waits for all 12 jobs to be `DONE`, and checks that the log has exactly 12 × 120 frames (no job analyzed twice) and that at least 2 different workers made them.

### Why your design just broke

With `FOR UPDATE`, every worker that reads the same "oldest pending job" waits on the first one's lock, so three workers behave like one, and if the claimer stalls they all stall. Without any lock, two workers both see the job as `PENDING` and both run it. `SKIP LOCKED` is the third option: a locked row is invisible to the others, so each worker takes a different row. It's one clause; the lesson is why the other two are wrong.

### Pointers

- [`SKIP LOCKED` in the `SELECT` docs](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE), and the 2ndQuadrant post "What is SKIP LOCKED for in PostgreSQL 9.5?".
- Search: `postgres queue skip locked`, `read committed lost update`.
