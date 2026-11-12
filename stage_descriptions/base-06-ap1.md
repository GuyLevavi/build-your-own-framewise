In this stage, you'll start an HTTP API that accepts video uploads as a stream.

From here on, Framewise is a service. Uploads are the first thing it takes from strangers, so they get limits on type and size, and they must never be read into memory whole.

### New here?

- **HTTP API**: a program that answers requests such as `GET /videos/1` with a status code (200 ok, 404 not found, 413 too large, 415 wrong type) and a body, here JSON.
- **FastAPI**: the web framework you'll use; **uvicorn** is the server that runs it.
- **Multipart upload**: how a browser or `curl -F file=@street.avi` sends a file in a request.
- **Streaming**: handling data in chunks as it arrives, so a 1 GB upload never sits in memory at once.

### Expected behaviour

`./your_program.sh serve --port PORT --data-dir DIR` runs the server on 127.0.0.1:PORT and keeps files under DIR.

- `GET /health` answers `200 {"status": "ok"}`.
- `POST /videos` with a multipart field `file` stores the upload and answers `201 {"video_id": "...", "filename": "street.AVI", "size_bytes": 678132}`. Every upload gets its own id.
- `GET /videos/{video_id}` answers `200` with the same fields, or `404` for an unknown id.
- A filename whose extension isn't `.avi`, `.mp4`, `.mkv` or `.mov` (in any case) gets `415`.
- An upload larger than `FRAMEWISE_MAX_UPLOAD_MB` MiB (default 1024) gets `413`, and nothing of it is kept under DIR.
- Every error body is JSON with a `detail` field. FastAPI's `HTTPException` does that for you.

### Tests

The tester starts your server and waits up to 30 s for `/health`. It uploads two clips, reads them back, and tries an unknown id and two bad file types. With `FRAMEWISE_MAX_UPLOAD_MB=200`, it streams a 150 MiB upload, which may grow your server's memory by at most 60 MiB, and then one byte over 200 MiB, which must get `413`.

From this stage on, the tester also sets `FRAMEWISE_PROFILES` (a profiles file like an3's) and `DATABASE_URL` for every server it starts. You can ignore them until they matter.

### Pointers

- FastAPI tutorial: [Request Files](https://fastapi.tiangolo.com/tutorial/request-files/) and [Handling Errors](https://fastapi.tiangolo.com/tutorial/handling-errors/).
- Search: `UploadFile file.read chunk`, `shutil.copyfileobj`.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

A `serve` subcommand that builds a FastAPI app and calls `uvicorn.run(app, host="127.0.0.1", port=...)`. Keep building the app in a function (`create_app(settings)`) so tests and later stages can pass it what it needs.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

`@app.post("/videos", status_code=201)` taking `file: UploadFile`. Check the suffix first (415). Then `while chunk := file.file.read(1 << 20):` write to `data_dir / f"{video_id}{suffix}"`, adding up the size, and raise `HTTPException(413)` the moment it passes the limit. Keep `{video_id: record}` in a dict for `GET /videos/{video_id}`; `uuid.uuid4().hex` makes ids.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`await file.read()` with no size reads the whole upload into memory. When you reject an upload halfway, delete the partial file (`try` / `except` around the write loop, `path.unlink(missing_ok=True)`, then re-raise).

</details>
