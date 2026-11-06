In this stage, you'll make the server survive a restart.

So far everything lives in memory: restart the server and every upload and job is gone, including jobs that were accepted but never run.

### Expected behaviour

Start the server again with the same `--data-dir` and:

- uploads made before the restart are still there (`GET /videos/{id}` gives `200`);
- finished jobs are unchanged (`GET /jobs/{id}`: same status, same segments);
- jobs that were `PENDING` or `RUNNING` when the server stopped are picked up again and reach `DONE`.

### Tests

The tester runs a job to `DONE`, uploads the long clip and submits two jobs on it, then stops the server with `SIGTERM` right away. It starts a new server on the same `--data-dir` and checks the three points above, then runs one more job.

### Notes

- `sqlite3` is in the standard library. One file under `--data-dir`, with a `videos` and a `jobs` table, is enough. Open a connection per operation; a connection must not be shared across threads.
- On startup, find unfinished jobs and queue them again. A job left `RUNNING` was interrupted, not done.
