In this stage, you'll decide what each kind of model-server failure means for a job: try again, or fail it with a reason.

The model server is another team's service on other machines, and it fails in its own ways. It gets busy and answers 503. It restarts, and connections are refused for a while. It hangs, and a request without a timeout waits forever while your heartbeat keeps the job's lease alive, so nobody else takes it either. And sometimes the model itself fails on an input, and sending that input again fails again.

### New here?

- **Timeout**: the longest you wait for an answer before giving up on it. Without one, a hung server hangs your worker.
- **Retryable error**: one that may go away if you try again (busy 503, connection refused, timeout). A 400 or 500 for this input will fail again.
- **Exponential backoff**: waiting longer after each failed try (0.25 s, 0.5 s, 1 s, ...), so a struggling server gets room to recover.

### Expected behaviour

- `FRAMEWISE_MODEL_TIMEOUT_S` (default your choice) bounds how long a worker waits for any one answer from the model server.
- A busy answer (503) or a request that couldn't get through (refused, reset, timed out) is retried a few times, with a growing pause between tries. If it keeps failing, the job ends `FAILED` with an `"error"` that names the model server.
- An answer that says the request itself is wrong or that the model failed on it (400 or 500) is not retried more than once or twice. The job ends `FAILED` with that answer's message.
- The worker keeps running through all of it, and its next job succeeds once the server is healthy.
- A retried request's detections are stored once.

### Tests

The tester runs with `FRAMEWISE_MODEL_TIMEOUT_S=2`, `FRAMEWISE_LEASE_S=3` and `FRAMEWISE_MAX_ATTEMPTS=2`, and runs four jobs with the remote profile:

1. The server answers every third request with 503. The street clip must reach `DONE`, with exactly one set of rows.
2. `poison.avi`: the server answers 500 for the batch holding the grey frame. The job must end `FAILED`.
3. The server is gone. The job must end `FAILED` within 45 s.
4. The server accepts requests and never answers. The job must end `FAILED` within 45 s.

After each failure it checks that the worker is still running, and at the end that a job succeeds again.

### Why your design just broke

Until now a job failed because of its input (pb2's bad files) or because the worker died (wk2's leases). A dependency is a third kind of failure, and it needs a policy: which errors are worth another try, how many tries, how long to wait, and what the job's error should say. Keep that policy in one place, the client, rather than in `try` blocks around each call. Note that retrying a whole job is the wrong tool for a 503 on one batch: every attempt would meet the next 503.

### Pointers

- Google SRE book, ["Handling Overload"](https://sre.google/sre-book/handling-overload/) (retries and their budgets) and AWS's ["Timeouts, retries, and backoff with jitter"](https://aws.amazon.com/builders-library/timeouts-retries-and-backoff-with-jitter/).
- `urllib.request.urlopen(..., timeout=...)` and `urllib.error.HTTPError.code`.
- Search: `retryable vs non-retryable http status`, `exponential backoff jitter`, `circuit breaker pattern`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

Put the whole policy in the client: every request goes through one method that sorts failures into "try again" and "give up", retries the first kind a few times with a growing pause, and raises your job-failing error with a clear message otherwise.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

`urlopen(request, timeout=timeout_s)`. A private `class _Retryable(Exception)`; `_send` raises it for `HTTPError` with code 429/502/503/504 and for `URLError`, `TimeoutError` and `ConnectionError`, and raises `ModelError` (a `FramewiseError`) for any other HTTP error, with the server's message. Then:

```python
def _call(self, attempt):
    for n in range(RETRIES):
        try:
            return attempt()
        except _Retryable as e:
            if n == RETRIES - 1:
                raise ModelError(f"model server at {self.url} failed {RETRIES} times: {e}")
            time.sleep(0.25 * 2**n)
```

</details>

<details>
<summary>Hint 3: the usual traps</summary>

A timeout raised while *reading* the answer is a `TimeoutError` (or `socket.timeout`), not a `URLError`: catch both. Keep the total retry time well under the tester's 45 s, and remember each try can take the full timeout.

</details>
