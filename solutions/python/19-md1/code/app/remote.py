"""The detector on a model server: Triton's HTTP API (the KServe v2 protocol).

The model is a black box with a contract, read from the server rather than
assumed: its input size and pixel format. This module turns frames into
requests and answers back into boxes in the video's own coordinates.
"""

import json
import urllib.error
import urllib.request
from collections.abc import Callable, Iterator
from dataclasses import dataclass
from pathlib import Path

import cv2
import fwdetect
import numpy as np

from app.analysis import FrameResult, Interrupted
from app.errors import FramewiseError
from app.profiles import Profile
from app.video import frames, open_video

MODEL = "fwdetect"
LABELS = ("person", "car", "bicycle")  # the model's class ids, in order


class ModelError(FramewiseError):
    """The model server can't analyze this job: the job fails with this message."""


@dataclass(frozen=True)
class ModelInfo:
    width: int
    height: int


class ModelClient:
    def __init__(self, url: str) -> None:
        self.url = url.rstrip("/")
        self._info: ModelInfo | None = None

    def info(self) -> ModelInfo:
        """The model's input size, from its metadata (asked once)."""
        if self._info is None:
            metadata = self._send(urllib.request.Request(f"{self.url}/v2/models/{MODEL}"))
            _, height, width, _ = metadata["inputs"][0]["shape"]
            self._info = ModelInfo(width, height)
        return self._info

    def detect(self, images: list[np.ndarray]) -> list[list[tuple[str, float, int, int, int, int]]]:
        """Boxes for a batch of RGB images at the model's input size, in its pixels."""
        batch = np.ascontiguousarray(np.stack(images))
        raw = batch.tobytes()
        header = json.dumps(
            {
                "inputs": [
                    {
                        "name": "images",
                        "datatype": "UINT8",
                        "shape": list(batch.shape),
                        "parameters": {"binary_data_size": len(raw)},
                    }
                ],
                "outputs": [{"name": "detections", "parameters": {"binary_data": False}}],
            }
        ).encode()
        request = urllib.request.Request(
            f"{self.url}/v2/models/{MODEL}/infer",
            data=header + raw,
            headers={
                "Content-Type": "application/octet-stream",
                "Inference-Header-Content-Length": str(len(header)),
            },
        )
        answer = self._send(request)
        output = next(o for o in answer["outputs"] if o["name"] == "detections")
        rows = np.asarray(output["data"], dtype=np.float32).reshape(output["shape"])
        return [
            [
                (LABELS[int(r[0])], float(r[1]), int(r[2]), int(r[3]), int(r[4]), int(r[5]))
                for r in image
                if r[0] >= 0
            ]
            for image in rows
        ]

    def _send(self, request: urllib.request.Request) -> dict:
        try:
            with urllib.request.urlopen(request) as response:
                return json.load(response)
        except urllib.error.HTTPError as e:
            detail = e.read().decode(errors="replace")[:200]
            raise ModelError(f"model server answered {e.code}: {detail}") from None
        except urllib.error.URLError as e:
            raise ModelError(f"model server at {self.url} unreachable: {e.reason}") from None


def remote_frames(
    path: Path, profile: Profile, client: ModelClient, should_stop: Callable[[], bool]
) -> Iterator[FrameResult]:
    """Every sampled frame's result, in order, from the model server, one frame a request."""
    info = client.info()
    with open_video(path) as (capture, video):
        scale_x, scale_y = video.width / info.width, video.height / info.height
        for index, frame in frames(capture, profile.every):
            if should_stop():
                raise Interrupted
            [boxes] = client.detect([_prepare(frame, info)])
            yield FrameResult(index, index / video.fps, [_box(b, scale_x, scale_y) for b in boxes])


def _prepare(frame: np.ndarray, info: ModelInfo) -> np.ndarray:
    """A BGR frame from OpenCV as the model takes it: its size, RGB."""
    if frame.shape[1] != info.width or frame.shape[0] != info.height:
        shrink = frame.shape[1] > info.width
        frame = cv2.resize(
            frame,
            (info.width, info.height),
            interpolation=cv2.INTER_AREA if shrink else cv2.INTER_LINEAR,
        )
    return cv2.cvtColor(frame, cv2.COLOR_BGR2RGB)


def _box(row: tuple[str, float, int, int, int, int], sx: float, sy: float) -> fwdetect.Box:
    label, score, x, y, w, h = row
    return fwdetect.Box(label, round(x * sx), round(y * sy), round(w * sx), round(h * sy), score)
