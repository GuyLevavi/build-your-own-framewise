In this stage, you'll start an HTTP server.

### Expected behaviour

```
$ ./your_program.sh serve --port 8000 --data-dir /tmp/fw-data
```

- `serve` runs until it is stopped, listening on `127.0.0.1` at `--port`.
- `--data-dir` is an existing, empty directory where the server may keep files. You don't need it yet, but accept the flag.
- `GET /health` answers `200` with `{"status": "ok"}`.

### Tests

The tester picks a free port, starts your server and polls `GET /health` for up to 30 seconds. It expects `200` and the JSON body above.

### Notes

- FastAPI and Uvicorn are installed. `uvicorn.run(app, host=..., port=...)` starts a server from inside `main()`.
- Import FastAPI only inside the `serve` command, so `probe` and `analyze` stay quick to start.
