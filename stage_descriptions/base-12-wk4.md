In this stage, you'll make a worker stop cleanly on SIGTERM and hand its job back.

Every deploy and every scale-down stops your workers with SIGTERM, then SIGKILL a little later. A lease means the job isn't lost, but with a lease long enough for real work it sits idle until the lease runs out, and the attempt counts against it.

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

Prepares for Framewise week 8.
