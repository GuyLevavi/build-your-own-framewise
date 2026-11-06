In this stage, you'll protect the API with a key.

### Expected behaviour

When the environment variable `FRAMEWISE_API_KEY` is set:

- every request except `GET /health` must carry the header `X-API-Key` with exactly that value;
- a missing or wrong key gives `401`, before any other check (so an unknown job id without a key is `401`, not `404`).

When it is not set, nothing changes: earlier stages run without it.

```
$ FRAMEWISE_API_KEY=s3cret ./your_program.sh serve --port 8000 --data-dir /tmp/fw
$ curl -i localhost:8000/jobs/x                          # 401
$ curl -i -H 'X-API-Key: s3cret' localhost:8000/jobs/x   # 404
```

### Tests

The tester starts the server with a random key and checks `/health` without a key (`200`), then `GET /jobs/{id}` with no key, a wrong key, half the key and the key plus one character (all `401`), and the right key (`404`). An upload without a key must give `401`. Finally it runs a whole job sending the key.

### Notes

- Read the key from the environment, never from a file in the repo.
- Compare with `hmac.compare_digest`, which takes the same time however many characters match.
- A middleware sees every request before routing and before the body is parsed.
