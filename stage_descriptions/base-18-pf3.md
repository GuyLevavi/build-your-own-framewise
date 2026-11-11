In this stage, you'll keep a worker alive and honest when one of its pool processes crashes.

`fwdetect` aborts the process on a dropped-feed frame. Before pf2 that killed the worker, its lease ran out, and the attempts limit failed the job. Now the abort kills a pool process instead, and the worker survives, which sounds better until you see what your pool does next.

### Expected behaviour

- With `FRAMEWISE_DETECT_PROCESSES` above 1, a job whose frames crash the detector ends `FAILED` with a non-empty `"error"` within a minute, after no more than `FRAMEWISE_MAX_ATTEMPTS` attempts.
- The worker keeps running, and its next job succeeds.

### Tests

The tester runs one worker with 2 or more processes, `FRAMEWISE_LEASE_S=3` and `FRAMEWISE_MAX_ATTEMPTS=2`, and submits `poison.avi`. The job must be `FAILED` within 60 s, the worker must still be running, and a job on the street clip must then succeed.

### Why your design just broke

`multiprocessing.Pool` replaces a process that died, but the task it was running is lost and its result never arrives: `map` and `imap` wait forever, while your heartbeat keeps the lease alive. `concurrent.futures.ProcessPoolExecutor` fails every pending future with `BrokenProcessPool` instead, after which the executor is unusable and must be replaced. Decide what a crash in a pool process means for the job: it's the same thing as a worker crash, so it should cost an attempt and go back to the queue, and the attempts limit you built in wk3 does the rest.

### Pointers

- [`BrokenProcessPool`](https://docs.python.org/3/library/concurrent.futures.html#concurrent.futures.process.BrokenProcessPool) and CPython issue "Pool hangs if a worker process dies unexpectedly" (bpo-22393).
- Search: `ProcessPoolExecutor recreate after BrokenProcessPool`, `supervisor pattern`.
