In this stage, you'll give claims a lease so a job outlives the worker that died running it.

Workers die: the node is drained, the container hits its memory limit, or the model runtime itself crashes, as native code does on an input it can't handle. (The tester makes onnxruntime abort the process when it's handed a mid-grey frame, the way a camera's dropped feed looks.) A job whose worker died is `RUNNING` forever, and nobody will ever take it.

### Expected behaviour

- A claim holds the job for a lease of `FRAMEWISE_LEASE_S` seconds (default your choice). A worker renews the lease while it works, from a thread, since the analysis loop is busy.
- A `RUNNING` job whose lease ran out is claimable again, like a `PENDING` one.
- Each claim counts an attempt. `GET /jobs/{id}` has `"attempts"`: how many times a worker has taken the job.
- A job that has used `FRAMEWISE_MAX_ATTEMPTS` attempts (default your choice) and lost its worker again is `FAILED`, with an `"error"` that says so. It must not be retried forever.

### Tests

The tester runs with `FRAMEWISE_LEASE_S=3` and `FRAMEWISE_MAX_ATTEMPTS=2`, starts 2 workers and restarts any worker that exits, as Kubernetes would. It submits the long clip with the `traffic` profile, waits for the detector to run, and kills that worker with SIGKILL. The job must reach `DONE` with the right labels and `"attempts": 2`. It then submits `poison.avi`, a clip with a frame that crashes the detector: the job must end `FAILED` with `"attempts": 2` and a non-empty `"error"`. A last job on the street clip must still succeed.

### Why your design just broke

"Claimed" was a status; now it is a status plus a deadline that someone has to keep moving. That puts a second thread in the worker (the heartbeat) and a second condition in the claim query (`RUNNING` with an expired lease). A worker that finds its lease gone (because it was too slow to renew and someone else took the job) should stop working on it. Think about what "attempt" means: the poison job never fails with an exception you can catch, so the only place that can give up on it is the claim.

### Pointers

- Martin Kleppmann, *Designing Data-Intensive Applications*, chapter 8, "Process pauses" and "Leases".
- [Python `threading.Event`](https://docs.python.org/3/library/threading.html#event-objects) for a heartbeat you can stop.
- Search: `visibility timeout`, `job queue lease heartbeat`, `poison message`.
