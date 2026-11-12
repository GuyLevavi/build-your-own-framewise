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


@cache
def _session(model: str):
    import onnxruntime  # only when a frame needs it

    path = MODELS / f"{model}.onnx"
    return onnxruntime.InferenceSession(str(path), providers=["CPUExecutionProvider"])


@cache
def labels(model: str) -> tuple[str, ...]:
    """The model's labels, by class id."""
    return tuple((MODELS / f"{model}.labels.txt").read_text().splitlines())


def _tensor(image: np.ndarray) -> np.ndarray:
    """A BGR HWC image as the models take it: RGB CHW floats in 0..1, a batch of one."""
    return image[:, :, ::-1].transpose(2, 0, 1)[None].astype(np.float32) / 255


def detect(frame: np.ndarray, size: int = SIZE) -> list[Box]:
    """What the detector finds in one BGR frame (as OpenCV reads it), run at size x size,
    in the frame's pixels. The only code that knows the sizes of both."""
    # Letterbox: scale to fit size x size keeping the aspect ratio, centred on grey 114.
    h, w = frame.shape[:2]
    scale = min(size / h, size / w)
    nw, nh = round(w * scale), round(h * scale)
    top, left = (size - nh) // 2, (size - nw) // 2
    canvas = np.full((size, size, 3), 114, np.uint8)
    canvas[top : top + nh, left : left + nw] = cv2.resize(frame, (nw, nh))
    rows = _session(DETECT).run(None, {"images": _tensor(canvas)})[0][0]
    names = labels(DETECT)
    return [
        Box(
            names[int(c)],
            round(float(s), 3),
            round((x1 - left) / scale),
            round((y1 - top) / scale),
            round((x2 - x1) / scale),
            round((y2 - y1) / scale),
        )
        for x1, y1, x2, y2, s, c in rows
        if s >= MIN_SCORE
    ]


def classify(frame: np.ndarray) -> list[Box]:
    """The classifier's top class for one BGR frame, if it is sure enough, as a whole-frame box."""
    # Shorter side to CLASSIFY_SIZE, then the square in the middle.
    h, w = frame.shape[:2]
    scale = CLASSIFY_SIZE / min(h, w)
    resized = cv2.resize(frame, (round(w * scale), round(h * scale)))
    top = (resized.shape[0] - CLASSIFY_SIZE) // 2
    left = (resized.shape[1] - CLASSIFY_SIZE) // 2
    square = resized[top : top + CLASSIFY_SIZE, left : left + CLASSIFY_SIZE]
    probs = _session(CLASSIFY).run(None, {"images": _tensor(square)})[0][0]
    best = int(probs.argmax())
    if probs[best] < MIN_SCORE:
        return []
    return [Box(labels(CLASSIFY)[best], round(float(probs[best]), 3), 0, 0, w, h)]


def run(model: str, frame: np.ndarray, size: int = SIZE) -> list[Box]:
    """The model on one frame; size is the detector's input (the classifier's is fixed)."""
    return classify(frame) if model == CLASSIFY else detect(frame, size)
