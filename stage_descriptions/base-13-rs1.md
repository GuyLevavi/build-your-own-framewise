In this stage, you'll store every box in Postgres and serve a time range of them, through an index.

So far a job's result is one JSON blob of segments. Anyone who wants to ask a different question (where was the car at 3 s? how often do two things overlap?) has to rerun the analysis. The raw detections are the data; segments are one view of it.

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

Prepares for Framewise weeks 4 and 10.
