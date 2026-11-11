In this stage, you'll protect the API with a key, compared the way secrets should be.

Anyone who can reach the service can upload videos and read results. Inside the cluster that's every pod.

### Expected behaviour

- When `FRAMEWISE_API_KEY` is set, every request needs the header `X-API-Key` with that value, except `GET /health` and `GET /ready` (probes carry no key). A missing or wrong key is 401 with a JSON `"detail"`.
- When it isn't set, nothing changes.
- The key is never written to logs.

### Tests

The tester sets a key and checks 401 without a key and with a key that differs only in its last character, on `GET /jobs/{id}`, `GET /videos/{id}`, `GET /metrics` and `POST /videos`; and 200 on `/health` without one. It then sends the key and runs a job end to end. Finally it stops the server and the worker and checks that neither's stderr contains the key.

The tester can't measure what a reviewer will check: compare the key in constant time.

### Why your design just broke

If authentication is a check pasted at the top of each handler, the next endpoint will forget it. Make it one piece applied to everything, with the exceptions listed in one place. `==` on strings returns as soon as a character differs, so the response time tells an attacker how much of a guess was right; `hmac.compare_digest` doesn't. Settings that hold secrets shouldn't print them either.

### Pointers

- [`hmac.compare_digest`](https://docs.python.org/3/library/hmac.html#hmac.compare_digest), and [FastAPI security](https://fastapi.tiangolo.com/tutorial/security/) (dependencies vs middleware).
- OWASP: [API2:2023 Broken Authentication](https://owasp.org/API-Security/editions/2023/en/0xa2-broken-authentication/).
- Search: `timing attack string comparison`, `kubernetes secret environment variable`.

Prepares for Framewise week 10.
