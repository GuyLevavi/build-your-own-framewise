In this stage, you'll move job execution out of the API into `work` processes, and let several of them share one queue without two taking the same job.

Your server runs jobs on a thread inside itself. That ties two things together that scale and fail differently: the API should answer in milliseconds and restart freely, while analysis is slow, CPU-bound and wants more machines. From now on `serve` only records jobs, and workers run them. One worker analyzes one job at a time, so to go faster you start more, and then they compete for the same rows.

### New here?

- **Process**: a running program with its own memory. `serve` and `work` are separate processes; they share nothing but the database.
- **Transaction**: a group of statements that happen all together or not at all. Locks taken inside it last until it commits.
- **Row lock**: `SELECT ... FOR UPDATE` locks the rows it returns, so another transaction can't change them until yours ends.
- **`SKIP LOCKED`**: tells Postgres to skip rows someone else has locked instead of waiting for them.

### Expected behaviour

- `./your_program.sh work` runs until it is stopped. It reads `DATABASE_URL` and `FRAMEWISE_PROFILES` like `serve`, takes `PENDING` jobs from `queue.jobs`, marks each `RUNNING`, analyzes it and records `DONE` or `FAILED`, as the in-process worker did.
- `serve` no longer runs jobs. With no worker running, a submitted job stays `PENDING`.
- The two talk only through Postgres. A worker keeps going when the server dies, and a new server sees what the worker recorded.
- Any number of `work` processes can run at once. Each job is analyzed by exactly one of them.
- A worker never waits for a job another worker is in the middle of claiming: it takes the next free one.

### Tests

First, one worker. The tester starts `serve` with no worker, submits a job on the street clip and checks it is still `PENDING` 3 seconds later. It starts one `work` process and waits for the job to reach `DONE` with the right labels. It then submits a job on the long clip, waits for `RUNNING`, kills the server with SIGKILL, and checks the worker is still alive. It starts a new server and waits for that job to reach `DONE`.

Then, many workers. The tester watches onnxruntime inside your workers and logs every frame the detector runs on, with its process id. It submits 12 jobs on the street clip, then locks the first job's row in its own transaction, as a worker in the middle of claiming it would. It starts 3 workers and expects the other 11 jobs to finish while the lock is held. It then releases the lock, waits for all 12 jobs to be `DONE`, and checks that the log has exactly 12 × 120 frames (no job analyzed twice) and that at least 2 different workers made them.

Earlier stages now start a `work` process next to `serve` too, so they keep passing.

### Why your design just broke

The thread, its in-memory queue and the "requeue unfinished jobs at startup" step all go. The database is the queue now: a worker finds work with a query, and taking a job must be atomic: one transaction that picks a `PENDING` row, locks it and marks it `RUNNING`. If the repository from the last stage owns all SQL, the worker becomes a short loop over `claim()` and `finish()`, and `serve` loses code.

How you lock matters as soon as there are two workers. Without a lock, two workers both see the job as `PENDING` and both run it. With plain `FOR UPDATE`, every worker that reads the same "oldest pending job" waits on the first one's lock, so three workers behave like one, and if the claimer stalls they all stall. `SKIP LOCKED` is the third option: a locked row is invisible to the others, so each worker takes a different row. It's one clause; the lesson is why the other two are wrong.

### Pointers

- [PostgreSQL: explicit locking, row-level locks](https://www.postgresql.org/docs/current/explicit-locking.html#LOCKING-ROWS) and [`SELECT ... FOR UPDATE ... SKIP LOCKED`](https://www.postgresql.org/docs/current/sql-select.html#SQL-FOR-UPDATE-SHARE).
- [You Don't Need a Job Queue, Postgres Already Has SKIP LOCKED](https://www.prisma.io/blog/you-dont-need-a-job-queue-postgres-already-has-skip-locked): the claim query (skip the Prisma setup).
- Search: `postgres as a job queue`, `polling worker loop`, `read committed lost update`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to explain why a plain `FOR UPDATE`, and no lock at all, would both fail where `SKIP LOCKED` works, and to show how much code `serve` lost.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

The database is the queue. A worker is a loop: claim one job, run it, record the result, repeat; sleep briefly when there's nothing to claim. Claiming must be one transaction, so no two workers ever get the same job.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

The claim, in one `engine.begin()` block:

```sql
SELECT id FROM queue.jobs WHERE status = 'PENDING'
ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED;
UPDATE queue.jobs SET status = 'RUNNING' WHERE id = :id;
```

In SQLAlchemy Core: `sa.select(jobs.c.id).where(...).order_by(...).limit(1).with_for_update(skip_locked=True)`. Then `work` is `while True: job = store.claim(); ...`, and `serve` loses its thread and its startup requeue.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

Without `FOR UPDATE`, two workers read the same PENDING row and both run it. With `FOR UPDATE` but no `SKIP LOCKED`, they all queue behind the first one's lock. Do the analysis *after* the claim's transaction commits: holding a transaction open for minutes holds the lock and a connection with it.

</details>
