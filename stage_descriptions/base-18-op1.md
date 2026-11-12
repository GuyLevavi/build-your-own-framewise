In this stage, you'll write structured logs a log platform can search, report each job's frames per second, and tell Kubernetes when the API is ready for traffic.

When the service runs in the cluster nobody watches its terminal. The team searches logs in Splunk, which wants fields it can filter on, not sentences. The number they look for first is frames per second. And Kubernetes asks every pod two questions over HTTP, all the time: are you alive, and are you ready for traffic?

### New here?

- **Structured logs**: each log line is a JSON object with fields (`job_id`, `fps`), so a log platform such as Splunk can filter and chart them.
- **Liveness probe**: Kubernetes asks "are you alive?"; if not, it restarts the pod.
- **Readiness probe**: Kubernetes asks "can you take traffic now?"; if not, it stops sending requests to the pod, without restarting it.

### Expected behaviour

- `GET /jobs/{id}` has `"frames"` (how many frames the detector analyzed) and `"fps"` (frames per second of the job's run) once the job is `DONE`.
- Workers write logs to stderr as one JSON object per line, with at least `"level"` and `"message"`, and `"job_id"` on every line about a job. A worker logs at least when it takes a job and when it finishes it, and the line that says it finished has a number field `"fps"`.
- `GET /health` (liveness) answers 200 whatever the database does.
- `GET /ready` (readiness) answers 200 when the database is reachable and 503 when it isn't, within a few seconds either way. Once the database is back, it answers 200 again without a restart.

### Tests

Your program reaches Postgres through a proxy the tester controls. The tester checks `/ready` answers 200, then runs two jobs on the street clip. Each must report `"frames": 120` and an `"fps"` that fits the time the job took. It stops the worker and reads its stderr: every line that mentions the first job must be JSON with `level` and `message`, at least 2 must carry its `job_id`, and one must carry `fps`. Then it cuts the database off: `/health` must answer 200 and `/ready` 503, each within 5 s. It brings the database back, and `/ready` must answer 200 within 30 s.

### Why your design just broke

Log calls with the id pasted into the message (`log.info(f"job {id} done")`) can't be searched by field. Put context in fields instead (`extra=` in the standard library), and format all of it as JSON in one place, the handler, so call sites stay plain. Compute a job's speed where the work happens, in the worker, and store it with the job: then the API and the logs report the same number.

Database errors used to be bugs, answered with a 500 or not at all. Now one of them is an expected condition: readiness asks the database, and a database that's away is a 503, from one exception handler. Set a connect timeout too, since the default is to wait. Keep liveness away from the database: a liveness probe that fails during a database outage makes Kubernetes restart every healthy pod.

### Pointers

- [Python logging cookbook: structured logging](https://docs.python.org/3/howto/logging-cookbook.html#implementing-structured-logging) and [`logging.Formatter`](https://docs.python.org/3/library/logging.html#formatter-objects).
- [Kubernetes: liveness, readiness and startup probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/).
- FastAPI [custom exception handlers](https://fastapi.tiangolo.com/tutorial/handling-errors/#install-custom-exception-handlers), and libpq's `connect_timeout` (SQLAlchemy: `create_engine(..., connect_args={"connect_timeout": 3})`).
- Search: `splunk json logs fields`, `liveness vs readiness probe`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Three small pieces. A logging formatter that turns every record, including the fields passed with `extra=`, into one JSON line. Two columns on jobs (frames, seconds) that the worker fills when it finishes. A `/ready` endpoint that runs `SELECT 1`, plus one exception handler that turns database errors into 503.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

Formatter: `class JsonFormatter(logging.Formatter)` whose `format(record)` returns `json.dumps({"level": record.levelname, "message": record.getMessage(), **extras})`, where extras are the record's attributes that a plain `logging.makeLogRecord({})` doesn't have. Install it on a stderr handler with `logging.basicConfig(handlers=[h], force=True)`. Call sites: `log.info("finished job", extra={"job_id": job.id, "fps": fps})`. For readiness: `@app.exception_handler(sqlalchemy.exc.DBAPIError)` returning `JSONResponse({"detail": ...}, 503)`, and `create_engine(..., pool_pre_ping=True, connect_args={"connect_timeout": 3})`.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`fps` must be a number, not a string. Measure the time in the worker around the analysis, and store it, so the API and the logs report the same figure. `/health` must not touch the database.

</details>
