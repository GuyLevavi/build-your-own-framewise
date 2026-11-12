"""The detector on a model server: Triton's HTTP API (the KServe v2 protocol).

The server runs the same yolo26n.onnx as models/. It is a black box with a contract, read from
the server rather than assumed: its input size and how many images one request may carry. This module turns frames into requests (with
model.prepare, as in this process) and answers back into boxes in the video's own coordinates
(model.read), and decides what a failure means: a busy or unreachable server is worth a few
more tries; a request the model rejects or fails on is not.
"""

import json
import time
import urllib.error
import urllib.request
from collections.abc import Callable, Iterator
from dataclasses import dataclass
from itertools import batched
from pathlib import Path

import numpy as np

from app import model
from app.analysis import FrameResult, Interrupted
from app.errors import FramewiseError
from app.profiles import Profile
from app.video import frames, open_video

MODEL = "yolo26n"  # its name on the server


RETRIES = 3


class ModelError(FramewiseError):
    """The model server can't analyze this job: the job fails with this message."""


class _Retryable(Exception):
    """Worth another try: the server is busy, slow or away for a moment."""


@dataclass(frozen=True)
class ModelInfo:
    size: int  # the input is size x size
    max_batch: int  # images per request, at most


class ModelClient:
    def __init__(self, url: str, timeout_s: float) -> None:
        self.url = url.rstrip("/")
        self.timeout_s = timeout_s
        self._info: dict[str, ModelInfo] = {}

    def info(self, name: str) -> ModelInfo:
        """A model's contract, from its metadata and config (asked once)."""
        if name not in self._info:
            metadata = self._call(lambda: self._get(f"/v2/models/{name}"))
            config = self._call(lambda: self._get(f"/v2/models/{name}/config"))
            _, _, height, width = metadata["inputs"][0]["shape"]
            if height != width:
                raise ModelError(f"model {name} takes {width}x{height} images, not squares")
            self._info[name] = ModelInfo(width, max(config.get("max_batch_size", 1), 1))
        return self._info[name]

    def infer(self, name: str, images: np.ndarray) -> np.ndarray:
        """The model's output0 for a batch of prepared images (N x 3 x S x S float32)."""
        raw = memoryview(np.ascontiguousarray(images, dtype="<f4")).cast("B")  # no copy
        header = json.dumps(
            {
                "inputs": [
                    {
                        "name": "images",
                        "datatype": "FP32",
                        "shape": list(images.shape),
                        "parameters": {"binary_data_size": raw.nbytes},
                    }
                ],
                "outputs": [{"name": "output0", "parameters": {"binary_data": True}}],
            }
        ).encode()
        request = urllib.request.Request(
            f"{self.url}/v2/models/{name}/infer",
            data=[header, raw],  # sent one after the other, not joined into a third copy
            headers={
                "Content-Type": "application/octet-stream",
                "Content-Length": str(len(header) + raw.nbytes),
                "Inference-Header-Content-Length": str(len(header)),
            },
        )
        answer, data = self._call(lambda: self._send(request))
        output = next(o for o in answer["outputs"] if o["name"] == "output0")
        return np.frombuffer(data, dtype="<f4").reshape(output["shape"])

    def _get(self, path: str) -> dict:
        answer, _ = self._send(urllib.request.Request(self.url + path))
        return answer

    def _send(self, request: urllib.request.Request) -> tuple[dict, bytes]:
        """The answer's JSON, and the binary data after it if there is any."""
        try:
            with urllib.request.urlopen(request, timeout=self.timeout_s) as response:
                body = response.read()
                length = int(response.headers.get("Inference-Header-Content-Length", len(body)))
        except urllib.error.HTTPError as e:
            detail = e.read().decode(errors="replace")[:200]
            if e.code in (429, 502, 503, 504):
                raise _Retryable(f"{e.code} {detail}") from None
            raise ModelError(f"model server answered {e.code}: {detail}") from None
        except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
            reason = getattr(e, "reason", e)
            raise _Retryable(f"{type(reason).__name__}: {reason}") from None
        return json.loads(body[:length]), body[length:]

    def _call[T](self, attempt: Callable[[], T]) -> T:
        """attempt(), tried again after a growing pause while the failure is retryable."""
        for n in range(RETRIES):
            try:
                return attempt()
            except _Retryable as e:
                if n == RETRIES - 1:
                    raise ModelError(
                        f"model server at {self.url} failed {RETRIES} times; last: {e}"
                    ) from None
                time.sleep(0.25 * 2**n)
        raise AssertionError("unreachable")


def remote_frames(
    path: Path, profile: Profile, client: ModelClient, should_stop: Callable[[], bool]
) -> Iterator[FrameResult]:
    """Every sampled frame's result, in order, from the model server.

    The server's model has its own input size, which needn't be ours. Each request pays a fixed
    cost however few frames it carries, so frames go in batches as large as the model takes.
    One batch at a time: the next is read only once this one's answer is back, so at most one
    batch of frames is ever in memory.
    """
    info = client.info(MODEL)
    with open_video(path) as (capture, video):
        for batch in batched(frames(capture, profile.every), info.max_batch):
            if should_stop():
                raise Interrupted
            # One array for the batch, filled in place: 16 images at 640 x 640 are 79 MB.
            images = np.empty((len(batch), 3, info.size, info.size), np.float32)
            fits = []
            for k, (_, frame) in enumerate(batch):
                images[k], fit = model.prepare(frame, info.size)
                fits.append(fit)
            outputs = client.infer(MODEL, images)
            for (index, _), fit, output in zip(batch, fits, outputs, strict=True):
                yield FrameResult(index, index / video.fps, model.read(output, fit))
