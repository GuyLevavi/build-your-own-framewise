In this stage, you'll stream a job's progress to clients with server-sent events and Postgres LISTEN/NOTIFY.

A ten-minute video takes a while. Clients poll `GET /jobs/{id}`, which tells them nothing until the end, and polling every client every second is load for nothing.

### Expected behaviour

- `GET /jobs/{id}` has `"progress"`, from 0 to 1. It is 1 only when the job is `DONE`.
- Workers record progress as they store batches, and `NOTIFY framewise_progress` with a JSON payload holding at least `job_id` and `progress`.
- `GET /jobs/{id}/progress` answers `text/event-stream`: one `data: {"status", "progress"}` event right away, then one whenever the job advances. When the job is `DONE` or `FAILED` it sends that last event and ends the stream. A finished job's stream sends one event and ends. An unknown job is 404.

### Tests

The tester `LISTEN`s on `framewise_progress`, submits the long clip and reads the event stream. It expects progress never to go down, at least 3 `RUNNING` events with growing progress, and a last event `{"status": "DONE", "progress": 1}` followed by the end of the stream. It expects at least 3 notifications for the job. It then opens the stream of the finished job, and checks 404 for an unknown one.

### Why your design just broke

Every request so far was short. An event stream is a long-lived request, so it can't hold a worker thread or a pooled connection for minutes: it wants an async endpoint and a connection of its own that listens. NOTIFY is delivered when its transaction commits, which is the guarantee you want, so send it in the same transaction that stores the batch. Think about the gap between reading the job and starting to listen, and about what NOTIFY can't tell you (a worker that died says nothing).

### Pointers

- [PostgreSQL `NOTIFY`](https://www.postgresql.org/docs/current/sql-notify.html) and [psycopg: asynchronous notifications](https://www.psycopg.org/psycopg3/docs/advanced/async.html#asynchronous-notifications).
- [MDN: using server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events) and Starlette's `StreamingResponse`.
- Search: `sse vs websockets`, `listen notify race condition`.
