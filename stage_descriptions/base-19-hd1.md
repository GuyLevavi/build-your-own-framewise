In this stage, you'll protect the API with a key, compared the way secrets should be.

Anyone who can reach the service can upload videos and read results. Inside the cluster that's every pod.

### New here?

- **API key**: a shared secret the client sends in a header (`X-API-Key`). Enough inside a private network.
- **Middleware**: code that runs around every request, before the handler. One place to check something for all endpoints.
- **Timing attack**: guessing a secret by measuring how long the comparison takes. `==` stops at the first wrong character, so it leaks how much of a guess was right.

### Expected behaviour

- When `FRAMEWISE_API_KEY` is set, every request needs the header `X-API-Key` with that value, except `GET /health` and `GET /ready` (probes carry no key). A missing or wrong key is 401 with a JSON `"detail"`.
- When it isn't set, nothing changes.
- The key is never written to logs.

### Tests

The tester sets a key and checks 401 without a key and with a key that differs only in its last character, on `GET /jobs/{id}`, `GET /videos/{id}`, `GET /jobs/{id}/detections` and `POST /videos`; and 200 on `/health` without one. It then sends the key and runs a job end to end. Finally it stops the server and the worker and checks that neither's stderr contains the key.

The tester can't measure what a reviewer will check: compare the key in constant time.

### Why your design just broke

If authentication is a check pasted at the top of each handler, the next endpoint will forget it. Make it one piece applied to everything, with the exceptions listed in one place. `==` on strings returns as soon as a character differs, so the response time tells an attacker how much of a guess was right; `hmac.compare_digest` doesn't. Settings that hold secrets shouldn't print them either.

### Pointers

- [`hmac.compare_digest`](https://docs.python.org/3/library/hmac.html#hmac.compare_digest), and [FastAPI security](https://fastapi.tiangolo.com/tutorial/security/) (dependencies vs middleware).
- OWASP: [API2:2023 Broken Authentication](https://owasp.org/API-Security/editions/2023/en/0xa2-broken-authentication/).
- Search: `timing attack string comparison`, `kubernetes secret environment variable`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to show the one place that checks the key, how it compares keys, and that the key never appears in a log line or a repr.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

One check, applied to every request, with the open paths (the probes) listed in one place. Compare with a function whose time doesn't depend on where the strings differ.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

```python
@app.middleware("http")
async def check_key(request, call_next):
    if request.url.path not in {"/health", "/ready"}:
        given = request.headers.get("x-api-key", "").encode()
        if not hmac.compare_digest(given, expected):
            return JSONResponse({"detail": "missing or wrong API key"}, 401)
    return await call_next(request)
```

Register it only when `FRAMEWISE_API_KEY` is set.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

Raising `HTTPException` inside a middleware doesn't become a 401: return the response. A settings dataclass prints every field in its `repr`, key included, whenever it lands in a log or a traceback: `field(repr=False)`.

</details>
