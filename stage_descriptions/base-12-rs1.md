In this stage, you'll store every box in Postgres and serve a time range of them, through an index.

So far a job's result is one JSON blob of segments. Anyone who wants to ask a different question (where was the car at 3 s? how often do two things overlap?) has to rerun the analysis. The raw detections are the data; segments are one view of it.

### New here?

- **Batch insert**: one statement that inserts many rows. Much cheaper than one statement per row.
- **Index**: a sorted copy of some columns that lets Postgres find matching rows without reading the whole table.
- **`EXPLAIN`**: asks Postgres how it would run a query: an `Index Scan` uses an index, a `Seq Scan` reads everything.

### Expected behaviour

- A table `results.detections` with at least the columns `job_id`, `t_s`, `label`, `x`, `y`, `w`, `h`: one row per box per analyzed frame, for the labels in the job's profile.
- Workers write them while the job runs, in batches, not one row per statement and not all at the end.
- `GET /jobs/{id}/detections?from_s=2&to_s=4` answers `{"detections": [{"t_s", "label", "x", "y", "w", "h"}, ...]}` with the boxes in that time range, both ends included, sorted by `t_s` then `label`. Both parameters are optional. An unknown job is 404.
- A query for one job and a time range can use an index.

### Tests

The tester runs a job on the street clip and counts its rows in `results.detections`. It asks for 2 to 4 seconds and compares every box with the clip (positions within 2 px). It checks 404 for an unknown job. Finally it runs `EXPLAIN` on `SELECT * FROM results.detections WHERE job_id = ... AND t_s BETWEEN 2 AND 4` with sequential scans disabled, and fails if Postgres can only scan the whole table.

### Why your design just broke

The worker's loop now produces two things per frame: rows to store and sightings for the segments. If `analyze()` both reads the video and builds segments, it has to be split: one generator that yields per-frame results, and consumers that batch rows into the database and fold sightings into segments. A new migration adds the table; the old ones don't change.

### Pointers

- [PostgreSQL: multicolumn indexes](https://www.postgresql.org/docs/current/indexes-multicolumn.html) and [`EXPLAIN`](https://www.postgresql.org/docs/current/using-explain.html).
- SQLAlchemy: "executemany" with a list of dicts in [`Connection.execute`](https://docs.sqlalchemy.org/en/20/tutorial/dbapi_transactions.html#sending-multiple-parameters).
- Search: `postgres bulk insert performance`, `index column order equality range`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

The frame loop now feeds two consumers: one collects sightings for the segments, the other stores boxes. Make analysis yield one result per frame (index, time, boxes) and let the worker do both with each result.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

Migration `0003_detections.py` creates `results.detections (id identity, job_id, frame, t_s, label, x, y, w, h, score)` and `CREATE INDEX ... ON results.detections (job_id, t_s)`. The worker collects rows in a list and every 50 frames calls `conn.execute(sa.insert(detections), rows)` with a list of dicts, which SQLAlchemy sends as one batch. The endpoint is `SELECT ... WHERE job_id = :id AND t_s BETWEEN :from AND :to ORDER BY t_s, label`.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

Column order in the index matters: equality first (`job_id`), range second (`t_s`). An index on `(t_s, job_id)` can't serve this query well. Store only the profile's labels, and don't forget the last, partial batch.

</details>
