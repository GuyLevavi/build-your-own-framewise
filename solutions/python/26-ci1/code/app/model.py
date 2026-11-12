"""The models in models/, run with onnxruntime (models/README.md is their contract).

yolo26n finds objects and their boxes; yolo26n-cls names what a whole frame shows. Both answer
here as boxes in the frame's pixels: a classification is one box, the whole frame.
"""

from dataclasses import dataclass
from functools import cache
from pathlib import Path

import cv2
import numpy as np

MODELS = Path(__file__).resolve().parent.parent / "models"
DETECT, CLASSIFY = "yolo26n", "yolo26n-cls"
SIZE = 640  # the detector's input is SIZE x SIZE unless the caller asks for another
CLASSIFY_SIZE = 224  # the classifier's
MIN_SCORE = 0.5  # a detection, or a top class, counts from this score


@dataclass(frozen=True, slots=True)
class Box:
    label: str
    score: float
    x: int
    y: int
    w: int
    h: int


_threads = 0  # threads per model call; 0 is onnxruntime's default, one per core


def use_threads(n: int) -> None:
    """How many threads a model call may use in this process. Several processes each using
    every core only slow each other down. Call it before the first frame."""
    global _threads
    _threads = n


@cache
def _session(model: str):
    import onnxruntime  # only when a frame needs it

    options = onnxruntime.SessionOptions()
    options.intra_op_num_threads = _threads
    path = MODELS / f"{model}.onnx"
    return onnxruntime.InferenceSession(str(path), options, providers=["CPUExecutionProvider"])


@cache
def labels(model: str) -> tuple[str, ...]:
    """The model's labels, by class id."""
    return tuple((MODELS / f"{model}.labels.txt").read_text().splitlines())


@dataclass(frozen=True, slots=True)
class Fit:
    """How a frame was fitted into a model's input: scaled by scale, then shifted by (left, top)."""

    scale: float
    left: int
    top: int
    width: int  # the frame's
    height: int


def prepare(model: str, frame: np.ndarray, size: int = SIZE) -> tuple[np.ndarray, Fit]:
    """One BGR frame (as OpenCV reads it) as the model takes it: RGB, 3 x H x W floats in 0..1.

    The detector's input is size x size: the frame letterboxed, scaled to fit keeping its aspect
    ratio and centred on grey 114. The classifier's is CLASSIFY_SIZE square: the frame scaled
    so its shorter side fits, and the square in the middle. With read(), the only code that
    knows the sizes of both the frame and the model's input; the model can run here or on a
    model server in between.
    """
    h, w = frame.shape[:2]
    if model == CLASSIFY:
        scale = CLASSIFY_SIZE / min(h, w)
        resized = cv2.resize(frame, (round(w * scale), round(h * scale)))
        top = (resized.shape[0] - CLASSIFY_SIZE) // 2
        left = (resized.shape[1] - CLASSIFY_SIZE) // 2
        image = resized[top : top + CLASSIFY_SIZE, left : left + CLASSIFY_SIZE]
        fit = Fit(scale, -left, -top, w, h)
    else:
        scale = min(size / h, size / w)
        nw, nh = round(w * scale), round(h * scale)
        top, left = (size - nh) // 2, (size - nw) // 2
        image = np.full((size, size, 3), 114, np.uint8)
        image[top : top + nh, left : left + nw] = cv2.resize(frame, (nw, nh))
        fit = Fit(scale, left, top, w, h)
    return image[:, :, ::-1].transpose(2, 0, 1).astype(np.float32) / 255, fit


def read(model: str, output: np.ndarray, fit: Fit) -> list[Box]:
    """One image's output (the detector's 300 x 6 rows, or the classifier's 1000
    probabilities) as boxes in the frame's pixels; a classification is the whole frame."""
    if model == CLASSIFY:
        best = int(output.argmax())
        if output[best] < MIN_SCORE:
            return []
        return [
            Box(labels(CLASSIFY)[best], round(float(output[best]), 3), 0, 0, fit.width, fit.height)
        ]
    names = labels(DETECT)
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


def run(model: str, frame: np.ndarray, size: int = SIZE) -> list[Box]:
    """The model on one frame, in this process; size is the detector's input."""
    image, fit = prepare(model, frame, size)
    return read(model, _session(model).run(None, {"images": image[None]})[0][0], fit)
