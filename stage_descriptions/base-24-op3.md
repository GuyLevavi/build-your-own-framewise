In this stage, you'll keep the API and the workers up while the database is gone, and recover when it's back.

Databases restart: upgrades, failovers, a full disk. If the service falls over with them, Kubernetes restarts it in a loop and the outage is longer than it needed to be.

### Expected behaviour

- `GET /health` (liveness) answers 200 whatever the database does.
- `GET /ready` (readiness) answers 200 when the database is reachable and 503 when it isn't.
- Endpoints that need the database answer 503 with a JSON `"detail"` while it's away, within a few seconds, not a 500 and not a hang.
- Workers don't exit. They retry with backoff, and a job that was running when the database went away is finished later, with its results stored exactly once.
- Everything works again without restarts once the database is back.

### Tests

Your program reaches Postgres through a proxy the tester controls. The tester submits the long clip, waits for `RUNNING` and cuts the database off for 8 s. Meanwhile it checks `/health` 200, `/ready` 503 and `GET /jobs/{id}` 503, each within 5 s, and that the server and the worker are still running. After the database is back, `/ready` must be 200 within 30 s, the job must reach `DONE` with exactly one set of detections, the same worker must still be running, and a new job must succeed. `FRAMEWISE_LEASE_S` is 3.

### Why your design just broke

Database errors used to be bugs, caught by the "internal error" handler or not at all. Now they're an expected condition with their own handling in two places: one exception handler turns them into 503 in the API, and the worker's loop treats them as "back off and retry", leaving the job to its lease. The worker's catch-all must not record a job as failed because the database blinked. Set a connect timeout, too: the default is to wait.

### Pointers

- [Kubernetes: liveness, readiness and startup probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).
- [SQLAlchemy: dealing with disconnects](https://docs.sqlalchemy.org/en/20/core/pooling.html#dealing-with-disconnects) and libpq's `connect_timeout`.
- Search: `exponential backoff jitter`, `fastapi exception_handler`.

Prepares for Framewise week 8.
