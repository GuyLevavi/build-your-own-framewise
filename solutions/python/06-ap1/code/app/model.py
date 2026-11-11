"""The detector: models/yolo26n.pt, run with Ultralytics (models/README.md is its contract)."""

from dataclasses import dataclass
from functools import cache
from pathlib import Path

import numpy as np

MODELS = Path(__file__).resolve().parent.parent / "models"
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
def _detector():
    from ultralytics import YOLO  # PyTorch takes a while to import: only when a frame needs it

    return YOLO(str(MODELS / "yolo26n.pt"), task="detect")


def detect(frame: np.ndarray) -> list[Box]:
    """What the detector finds in one BGR frame (as OpenCV reads it), in the frame's pixels."""
    result = _detector()(frame, conf=MIN_SCORE, verbose=False)[0]
    return [
        Box(result.names[int(c)], round(s, 3), round(x1), round(y1), round(x2 - x1), round(y2 - y1))
        for (x1, y1, x2, y2), s, c in zip(
            result.boxes.xyxy.tolist(), result.boxes.conf.tolist(), result.boxes.cls.tolist()
        )
    ]
