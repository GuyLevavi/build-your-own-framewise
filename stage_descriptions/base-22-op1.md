In this stage, you'll expose Prometheus metrics, report each job's frames per second, and write structured logs a log platform can search.

When the service runs in the cluster nobody watches its terminal. The team searches logs in Splunk and may chart metrics in Grafana, and both need the service to speak their format. The number they look for first is frames per second.

### Expected behaviour

- `GET /metrics` serves Prometheus text format with at least:
  - `framewise_jobs{status="..."}`: how many jobs are in each status. Workers are other processes (and other pods), so this has to come from the database, not from counters in the server;
  - `framewise_frames_analyzed_total`: frames analyzed by finished jobs, also from the database;
  - a histogram `framewise_http_request_duration_seconds` with a `route` label holding the route's template, such as `/jobs/{job_id}`.
- `GET /jobs/{id}` has `"frames"` (how many frames the detector analyzed) and `"fps"` (frames per second of the job's run) once the job is `DONE`.
- No metric label holds an id.
- Workers write logs to stderr as one JSON object per line, with at least `"level"` and `"message"`, and `"job_id"` on every line about a job. A worker logs at least when it takes a job and when it finishes it, and the line that says it finished has a number field `"fps"`.

### Tests

The tester runs two jobs on the street clip. Each must report `"frames": 120` and an `"fps"` that fits the time the job took. It reads `/metrics`: `framewise_jobs{status="DONE"}` must be 2, `framewise_frames_analyzed_total` 240, the histogram must have a count for a route like `/jobs/{...}`, and neither job id may appear anywhere. It stops the worker and reads its stderr: every line that mentions the first job must be JSON with `level` and `message`, at least 2 must carry its `job_id`, and one must carry `fps`.

### Why your design just broke

Log calls with the id pasted into the message (`log.info(f"job {id} done")`) can't be searched by field. Put context in fields instead (`extra=` in the standard library), and format all of it as JSON in one place, the handler, so call sites stay plain. Compute a job's speed where the work happens, in the worker, and store it with the job: then the API, the metrics and the logs all report the same number. For metrics, the subtle part is cardinality: a label per raw path creates a time series per job and eventually takes Prometheus down.

### Pointers

- [prometheus_client](https://prometheus.github.io/client_python/) (custom collectors, multiprocess mode and why we avoid it here), and [Prometheus naming and label best practices](https://prometheus.io/docs/practices/naming/).
- [Python logging cookbook: structured logging](https://docs.python.org/3/howto/logging-cookbook.html#implementing-structured-logging).
- Search: `prometheus high cardinality labels`, `starlette route template middleware`.

Prepares for Framewise week 8.
