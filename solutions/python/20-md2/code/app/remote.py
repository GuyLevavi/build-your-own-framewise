"""The models on a model server: Triton's HTTP API (the KServe v2 protocol).

The server runs the same .onnx files as models/. Each model is a black box with a contract, read
from the server rather than assumed: its input size, its batch limit and how many requests it
runs at once. This module turns frames into requests (with model.prepare, as in this process)
and answers back into boxes in the video's own coordinates (model.read).
"""

import json
import urllib.error
import urllib.request
from collections import deque
from collections.abc import Callable, Iterator
from concurrent.futures import Future, ThreadPoolExecutor
from dataclasses import dataclass
from itertools import batched
from pathlib import Path

import numpy as np

from app import model
from app.analysis import FrameResult, Interrupted
from app.errors import FramewiseError
from app.profiles import Profile
from app.video import frames, open_video


class ModelError(FramewiseError):
    """The model server can't analyze this job: the job fails with this message."""


@dataclass(frozen=True)
class ModelInfo:
    size: int  # the input is size x size
    max_batch: int
    instances: int


class ModelClient:
    def __init__(self, url: str) -> None:
        self.url = url.rstrip("/")
        self._info: dict[str, ModelInfo] = {}

    def info(self, name: str) -> ModelInfo:
        """A model's contract, from its metadata and config (asked once)."""
        if name not in self._info:
            metadata = self._get(f"/v2/models/{name}")
            config = self._get(f"/v2/models/{name}/config")
            _, _, height, width = metadata["inputs"][0]["shape"]
            if height != width:
                raise ModelError(f"model {name} takes {width}x{height} images, not squares")
            instances = sum(group.get("count", 1) for group in config.get("instance_group", []))
            self._info[name] = ModelInfo(width, config["max_batch_size"], max(instances, 1))
        return self._info[name]

    def infer(self, name: str, images: np.ndarray) -> np.ndarray:
        """The model's output0 for a batch of prepared images (N x 3 x S x S float32)."""
        raw = np.ascontiguousarray(images, dtype="<f4").tobytes()
        header = json.dumps(
            {
                "inputs": [
                    {
                        "name": "images",
                        "datatype": "FP32",
                        "shape": list(images.shape),
                        "parameters": {"binary_data_size": len(raw)},
                    }
                ],
                "outputs": [{"name": "output0", "parameters": {"binary_data": True}}],
            }
        ).encode()
        request = urllib.request.Request(
            f"{self.url}/v2/models/{name}/infer",
            data=header + raw,
            headers={
                "Content-Type": "application/octet-stream",
                "Inference-Header-Content-Length": str(len(header)),
            },
        )
        answer, data = self._send(request)
        output = next(o for o in answer["outputs"] if o["name"] == "output0")
        return np.frombuffer(data, dtype="<f4").reshape(output["shape"])

    def _get(self, path: str) -> dict:
        answer, _ = self._send(urllib.request.Request(self.url + path))
        return answer

    def _send(self, request: urllib.request.Request) -> tuple[dict, bytes]:
        """The answer's JSON, and the binary data after it if there is any."""
        try:
            with urllib.request.urlopen(request) as response:
                body = response.read()
                length = int(response.headers.get("Inference-Header-Content-Length", len(body)))
        except urllib.error.HTTPError as e:
            detail = e.read().decode(errors="replace")[:200]
            raise ModelError(f"model server answered {e.code}: {detail}") from None
        except urllib.error.URLError as e:
            raise ModelError(f"model server at {self.url} unreachable: {e.reason}") from None
        return json.loads(body[:length]), body[length:]


def remote_frames(
    path: Path, profile: Profile, client: ModelClient, should_stop: Callable[[], bool]
) -> Iterator[FrameResult]:
    """Every sampled frame's result, in order, from the model server.

    The server's model has its own input size: the profile's input_size is for this process.
    Frames go in batches as large as the model takes, with as many requests in flight as it
    runs at once, plus one being filled. Reading waits while that many are out: frames nobody
    can take yet would only pile up in memory.
    """
    info = client.info(profile.model)
    in_flight = info.instances + 1
    with open_video(path) as (capture, video), ThreadPoolExecutor(in_flight) as pool:
        pending: deque[tuple[list[tuple[int, model.Fit]], Future[np.ndarray]]] = deque()

        def collect() -> Iterator[FrameResult]:
            fits, future = pending.popleft()
            for (index, fit), output in zip(fits, future.result(), strict=True):
                yield FrameResult(index, index / video.fps, model.read(profile.model, output, fit))

        try:
            for chunk in batched(frames(capture, profile.every), info.max_batch):
                if should_stop():
                    raise Interrupted
                prepared = [model.prepare(profile.model, frame, info.size) for _, frame in chunk]
                images = np.stack([image for image, _ in prepared])
                fits = [(index, fit) for (index, _), (_, fit) in zip(chunk, prepared, strict=True)]
                pending.append((fits, pool.submit(client.infer, profile.model, images)))
                if len(pending) >= in_flight:
                    yield from collect()
            while pending:
                yield from collect()
        finally:
            for _, future in pending:
                future.cancel()
