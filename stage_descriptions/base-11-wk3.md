In this stage, you'll make a worker stop cleanly on SIGTERM and hand its job back.

Every deploy and every scale-down stops your workers with SIGTERM, then SIGKILL a little later. A lease means the job isn't lost, but with a lease long enough for real work it sits idle until the lease runs out, and the attempt counts against it.

### New here?

- **SIGTERM**: the polite "please stop" signal. Kubernetes sends it before every deploy or scale-down, then SIGKILL if the process is still there after a grace period.
- **Signal handler**: a function Python runs when a signal arrives, between two bytecode instructions of whatever was running.

### Expected behaviour

- On SIGTERM, `work` stops between frames, returns its job to `PENDING` (or finishes it, if it was about to), and exits with code 0 within a few seconds.
- A job given back this way is not a failed attempt.
- An idle worker exits promptly on SIGTERM too.

### Tests

The tester runs one worker with `FRAMEWISE_LEASE_S=60`, submits the long clip, waits for the detector to start, and sends SIGTERM. The worker must exit within 5 seconds with code 0, and the job must be `PENDING` or `DONE`, not `RUNNING`. A new worker must then take it to `DONE`.

### Why your design just broke

Python's default for SIGTERM is to die on the spot. Handling it means a stop flag that the main loop and the analysis loop both check, so the analysis needs a way to be asked to stop between frames, and the repository needs a "give this back" operation that only applies if the job is still yours. The signal handler should only set the flag.

### Pointers

- [Python `signal`](https://docs.python.org/3/library/signal.html), especially "Execution of Python signal handlers".
- [Kubernetes: termination of pods](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination).
- Search: `graceful shutdown worker sigterm python`, `terminationGracePeriodSeconds`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

The handler only sets a flag. The worker's loop checks the flag before claiming; the analysis checks it between frames and stops early when it's set. A job stopped this way goes back to `PENDING`, and its attempt is given back.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

`stop = threading.Event()`; `signal.signal(signal.SIGTERM, lambda *_: stop.set())`. Pass a `should_stop` callable into the frame loop and `raise Interrupted` when it's true. On `Interrupted`, the repository's `release(job_id)` runs `UPDATE ... SET status = 'PENDING', lease_until = NULL, attempts = attempts - 1 WHERE id = :id AND status = 'RUNNING'`. Then exit 0.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

An idle worker sleeping with `time.sleep(poll)` won't notice the flag until it wakes: sleep with `stop.wait(poll)` instead. Don't do database work inside the signal handler itself.

</details>
