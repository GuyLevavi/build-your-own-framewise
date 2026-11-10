In this stage, you'll make a retried job store its results exactly once.

Leases and batched writes meet here. A worker that dies halfway through a job has already committed some batches of detections. The retry writes them all again, and every count, range and event built on that table is now wrong.

### Expected behaviour

- After a job is retried, `results.detections` holds exactly one set of rows for it: what one complete analysis produces.
- Everything from earlier stages still holds: detections and events are right after a retry.

### Tests

The tester runs one supervised worker with `FRAMEWISE_LEASE_S=3`, submits the long clip, waits until more than 100 rows are stored for the job, and kills the worker with SIGKILL. When the job is `DONE`, its row count must be exactly that of one full run, and its events must be right.

### Why your design just broke

Retries are only safe if what a job writes is idempotent. Several fixes work, with different costs: delete the job's rows when an attempt starts (in the same transaction as the claim), tag rows with the attempt and read only the latest, or make rows unique on (job, frame, label) and upsert. Pick one and be ready to say why in review. Whatever you pick, the claim and the cleanup must not be able to interleave with another worker.

### Pointers

- [`INSERT ... ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html#SQL-ON-CONFLICT).
- Search: `idempotent consumer`, `at-least-once delivery`, `exactly-once is at-least-once plus idempotence`.

Prepares for Framewise weeks 4 and 8.
