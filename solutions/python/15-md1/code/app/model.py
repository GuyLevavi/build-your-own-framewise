"""The detector, models/yolo26n.onnx, run with onnxruntime (models/README.md is its contract)."""

from dataclasses import dataclass
from functools import cache
from pathlib import Path

import cv2
import numpy as np

MODELS = Path(__file__).resolve().parent.parent / "models"
SIZE = 640  # the detector's input is SIZE x SIZE here; a model server may use another size
MIN_SCORE = 0.5  # a detection counts from this score


@dataclass(frozen=True, slots=True)
class Box:
    label: str
    score: float
    x: int
    y: int
    w: int
    h: int


@cache
def _session():
    import onnxruntime  # only when a frame needs it

    return onnxruntime.InferenceSession(
        str(MODELS / "yolo26n.onnx"), providers=["CPUExecutionProvider"]
    )


@cache
def labels() -> tuple[str, ...]:
    """The detector's labels, by class id."""
    return tuple((MODELS / "yolo26n.labels.txt").read_text().splitlines())


@dataclass(frozen=True, slots=True)
class Fit:
    """How a frame was fitted into the model's input: scaled by scale, then shifted by (left, top)."""

    scale: float
    left: int
    top: int


def prepare(frame: np.ndarray, size: int = SIZE) -> tuple[np.ndarray, Fit]:
    """One BGR frame (as OpenCV reads it) as the detector takes it: 3 x size x size RGB floats
    in 0..1, the frame letterboxed (scaled to fit keeping its aspect ratio, centred on grey 114).

    With read(), the only code that knows the sizes of both the frame and the model's input; the
    model can run here or on a model server in between.
    """
    h, w = frame.shape[:2]
    scale = min(size / h, size / w)
    nw, nh = round(w * scale), round(h * scale)
    top, left = (size - nh) // 2, (size - nw) // 2
    image = np.full((size, size, 3), 114, np.uint8)
    image[top : top + nh, left : left + nw] = cv2.resize(frame, (nw, nh))
    return image[:, :, ::-1].transpose(2, 0, 1).astype(np.float32) / 255, Fit(scale, left, top)


def read(output: np.ndarray, fit: Fit) -> list[Box]:
    """One image's output (300 rows of x1, y1, x2, y2, score, class) as boxes in the frame's
    pixels: the letterbox undone."""
    names = labels()
    return [
        Box(
            names[int(c)],
            round(float(s), 3),
            round((x1 - fit.left) / fit.scale),
            round((y1 - fit.top) / fit.scale),
            round((x2 - x1) / fit.scale),
            round((y2 - y1) / fit.scale),
        )
        for x1, y1, x2, y2, s, c in output
        if s >= MIN_SCORE
    ]


def detect(frame: np.ndarray) -> list[Box]:
    """What the detector finds in one BGR frame, in this process, in the frame's pixels."""
    image, fit = prepare(frame)
    return read(_session().run(None, {"images": image[None]})[0][0], fit)
