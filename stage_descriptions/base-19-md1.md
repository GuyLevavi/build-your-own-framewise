In this stage, you'll run the models on a model server instead of in your own process, chosen by a profile setting.

In production the models don't run inside your service. The research team's `.onnx` files sit on Triton Inference Server, on machines with GPUs, and your worker calls them over HTTP. They are the same black boxes as in `models/`, but now the server publishes their contract: each model's input name, type and shape, and the shape of what it returns. Read that contract from the server rather than copying numbers into your code: whoever runs the server chooses the input size it was deployed with, and it needn't be yours.

### Expected behaviour

- A profile may set `detector: remote` (the default is `local`, onnxruntime in your process). Any other value is an invalid setting, named like any other. The profile's `model` picks the model on the server as it does locally.

  ```yaml
  profiles:
    remote:
      every: 1
      max_gap_s: 0.5
      labels: [person, car, bicycle]
      detector: remote
    parked_remote:
      every: 2
      max_gap_s: 0.5
      model: yolo26n-cls
      labels: [sports_car, convertible]
      detector: remote
  ```

- `FRAMEWISE_MODEL_URL` holds the model server's base URL, such as `http://127.0.0.1:8000`. Workers read it like `DATABASE_URL`.
- A job whose profile is remote sends its sampled frames to the profile's model on the server and never runs a model locally. Its labels and stored detections are what the model finds at the server's input size, with boxes in the video's coordinates.
- Jobs with local profiles behave exactly as before.

The server speaks [Triton's HTTP API](https://github.com/triton-inference-server/server/blob/main/docs/protocol/README.md), the KServe v2 inference protocol, and serves `yolo26n` and `yolo26n-cls`:

- `GET /v2/models/yolo26n` is the model's metadata: the input `images` (`FP32`, shape `[-1, 3, S, S]`, where `-1` is the batch and S the size it was deployed at) and the output `output0` (`FP32`, `[-1, 300, 6]`). The tensors are the ones `models/README.md` describes: prepare an image as it says, at size S.
- `GET /v2/models/yolo26n/config` is its configuration: `max_batch_size`, `instance_group` and the same tensors in Triton's config format.
- `POST /v2/models/yolo26n/infer` runs it. The body is JSON, or JSON followed by the raw tensor bytes (little-endian float32), with the header `Inference-Header-Content-Length` giving the JSON's length: Triton's binary extension. Ask for `output0` with `"binary_data": true` and the answer comes back the same way. A request the model can't take gets 400 with a JSON `error`.

### Tests

The tester starts its own model server and puts its URL in `FRAMEWISE_MODEL_URL`. The detector's input size is 512 or 640, picked at random each run, and the server runs your `models/` files with onnxruntime. The tester runs the street clip with the `remote` profile and checks the labels, that the server analyzed each of the 120 frames exactly once, that the model never ran in your processes, and the boxes between 2 and 4 s against what the model finds at the server's size (an IoU of at least 0.85 each). It runs the 1080p clip the same way, then the parked clip with `parked_remote`. Finally it runs the street clip with the default profile and expects the model to run locally and the model server not to be called.

### Why your design just broke

Your worker has called the model in-process since stage 3. Now there are two places it runs, and the profile picks one. This is the shape the design rules ask for: an interface for things with swappable implementations, a setting for the choice, no subclass per variant. The remote side also needs a client that lives as long as the worker (it caches each model's metadata), which is state, so it's an object with an owner.

The good news is that preparing a frame and reading the answer back don't change: the tensors are the same, only the size comes from somewhere else. If your letterboxing and box reading are one pair of functions that take the size as an argument, the remote path reuses them, and there is still one place that knows how a frame becomes a tensor.

### Pointers

- Triton's [HTTP/REST protocol](https://github.com/triton-inference-server/server/blob/main/docs/protocol/README.md) and its [binary data extension](https://github.com/triton-inference-server/server/blob/main/docs/protocol/extension_binary_data.md). Sending a frame as JSON numbers works and is many times slower: a 640 x 640 input is 1.2 million floats.
- The standard library's `urllib.request` is enough here. `tritonclient[http]` speaks the same protocol, if your package mirror has it.
- Search: `numpy tobytes frombuffer`, `kserve v2 inference protocol`, `triton model configuration instance_group`.
