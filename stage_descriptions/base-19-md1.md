In this stage, you'll run the detector on a model server instead of in your own process, chosen by a profile setting.

In production the model doesn't run inside your service. The researchers' model sits on Triton Inference Server, on machines with GPUs, and your worker calls it over HTTP. The model is still a black box, but now its contract is published by the server: its input name, size and pixel format, and the shape of what it returns. Read that contract from the server rather than copying numbers into your code.

### Expected behaviour

- A profile may set `detector: remote` (the default is `local`, fwdetect in your process). Any other value is an invalid setting, named like any other.

  ```yaml
  profiles:
    remote:
      every: 1
      max_gap_s: 0.5
      labels: [person, car, bicycle]
      detector: remote
  ```

- `FRAMEWISE_MODEL_URL` holds the model server's base URL, such as `http://127.0.0.1:8000`. Workers read it like `DATABASE_URL`.
- A job whose profile is remote sends its sampled frames to the model `fwdetect` and never calls fwdetect locally. Its labels and stored detections are the same as a local job's, with boxes in the video's coordinates.
- Jobs with local profiles behave exactly as before.

The server speaks [Triton's HTTP API](https://github.com/triton-inference-server/server/blob/main/docs/protocol/README.md), the KServe v2 inference protocol:

- `GET /v2/models/fwdetect` is the model's metadata: the input `images` (`UINT8`, shape `[-1, H, W, 3]`, where `-1` is the batch) and the output `detections` (`FP32`, `[-1, 3, 6]`).
- `GET /v2/models/fwdetect/config` is its configuration: `max_batch_size`, `instance_group`, and `parameters` saying the pixels are RGB and what a detection row holds: `class_id, score, x, y, w, h`, in the pixels of the model's input. Class ids 0, 1 and 2 are person, car and bicycle; a row with `class_id` -1 is "no box".
- `POST /v2/models/fwdetect/infer` runs it. The body is JSON, or JSON followed by the raw tensor bytes with the header `Inference-Header-Content-Length` giving the JSON's length (Triton's binary extension). A request the model can't take gets 400 with a JSON `error`.

### Tests

The tester starts its own model server and puts its URL in `FRAMEWISE_MODEL_URL`. Its input size is not the street clip's: it is 512x288 or 768x432, picked at random each run. The tester runs the street clip with the `remote` profile and checks the labels, that the server analyzed each of the 120 frames exactly once, that fwdetect's trace stayed empty, and the boxes between 2 and 4 s (within 3 px). It runs the 1080p clip the same way (boxes within 8 px). Finally it runs the street clip with the default profile and expects fwdetect to run locally and the model server not to be called.

### Why your design just broke

Your worker has called `fwdetect.detect(frame)` directly since stage 3. Now there are two detectors, and the profile picks one. This is the shape the design rules ask for: an interface for things with swappable implementations, a setting for the choice, no subclass per variant. The remote detector also needs a client that lives as long as the worker (it caches the model's metadata), which is state, so it's an object with an owner.

The pre- and post-processing are where real bugs hide. OpenCV gives you BGR and the model reads RGB: get that wrong and a person comes back as a car. Resize to the model's size and scale its boxes back, and you have one more function that knows a frame's size (stage 16 had the first).

### Pointers

- Triton's [HTTP/REST protocol](https://github.com/triton-inference-server/server/blob/main/docs/protocol/README.md) and its [binary data extension](https://github.com/triton-inference-server/server/blob/main/docs/protocol/extension_binary_data.md). Sending frames as JSON numbers works and is many times slower.
- The standard library's `urllib.request` is enough here. `tritonclient[http]` speaks the same protocol, if your package mirror has it.
- Search: `numpy tobytes frombuffer`, `cv2.cvtColor COLOR_BGR2RGB`, `kserve v2 inference protocol`.

Prepares for Framewise week 5.
