In this stage, you'll give claims a lease so a job outlives the worker that died running it.

Workers die: the node is drained, the container hits its memory limit, or the model runtime itself crashes, as native code does on an input it can't handle. (The tester makes onnxruntime abort the process when it's handed a mid-grey frame, the way a camera's dropped feed looks.) A job whose worker died is `RUNNING` forever, and nobody will ever take it.

### New here?

- **Lease**: a claim with an expiry time. The worker must renew it while it works; if it stops (because it died), the lease runs out and the job is free again.
- **Heartbeat**: the periodic renewal, here a small background thread.
- **SIGKILL**: the signal that ends a process at once, with no chance to clean up. Crashes look like this.
- **Poison job**: one that crashes every worker that tries it. Retrying it forever takes the whole queue down.

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

### In review

The tester can't check this part, so a reviewer will: you'll be asked to say what happens to a job if the heartbeat thread dies while the analysis keeps going.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Add two columns to jobs, `lease_until` and `attempts`. A job is claimable when it is `PENDING`, or `RUNNING` with `lease_until < now()`. A claim first looks at `attempts`: if it has already reached the limit, the job fails; otherwise the claim adds one to `attempts` and sets `lease_until = now() + lease`.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

A new migration (`0002_job_leases.py`) adds the columns. The claim's `WHERE` becomes `status = 'PENDING' OR (status = 'RUNNING' AND lease_until < now())`. In the same transaction, check the count *before* adding one: a claim that finds `attempts >= max_attempts` marks the job `FAILED` with an error that says it gave up, without counting another attempt, and looks for the next job. With a limit of 2 that's two runs and `"attempts": 2`; adding one first and checking after gives 3. The heartbeat: a `threading.Thread` that every `lease / 3` seconds runs `UPDATE ... SET lease_until = now() + lease WHERE id = :id AND status = 'RUNNING'`, and stops on a `threading.Event`. If one renewal raises (the database blinked), log it with `exc_info=True` and try again next beat, rather than letting the thread die.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

The poison job never raises an exception you can catch: the process just disappears. So the only place that can count its failures is the claim. Use the database's clock (`now()`) for leases, not each worker's. If a renewal updates 0 rows, someone else has the job: stop working on it.

</details>
