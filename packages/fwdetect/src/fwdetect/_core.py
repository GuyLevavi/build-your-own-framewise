"""The detector itself. Course authors only: learners treat this as opaque.

It finds the coloured rectangles the course tester draws: each label owns one
colour channel (person red, car blue, bicycle green), so a box is the bounding
box of the pixels where that channel is bright. The rest is a cost model that
stands in for a real network: work proportional to the pixel count, most of it
in pure Python so it holds the GIL.
"""

from __future__ import annotations

import json
import os
import time
from dataclasses import dataclass

import numpy as np
import numpy.typing as npt

LABELS = ("person", "car", "bicycle")
MIN_WIDTH = 320

# BGR channel index of each label's colour.
_CHANNEL = {"person": 2, "car": 0, "bicycle": 1}
_BRIGHT = 160
_MIN_PIXELS = 16

# One pure-Python step per this many pixels: roughly 10 ms for a 640x360 frame
# on a current x86 core and 9 times that at 1080p, so the detector, not
# decoding, dominates a job, as a real model would.
_PIXELS_PER_STEP = 2

_POISON_GREY = 128


@dataclass(frozen=True, slots=True)
class Box:
    label: str
    x: int
    y: int
    w: int
    h: int
    score: float


def detect(frame: npt.NDArray[np.uint8]) -> list[Box]:
    """Detect people, cars and bicycles in one BGR frame."""
    _check(frame)
    if _is_dropped_feed(frame):
        os.abort()
    checksum = _cost(frame)
    boxes = [box for label in LABELS if (box := _find(frame, label)) is not None]
    if checksum < 0:  # never true; keeps the cost from looking like dead code
        boxes.clear()
    _trace(frame, boxes)
    return boxes


def _check(frame: npt.NDArray[np.uint8]) -> None:
    if not isinstance(frame, np.ndarray) or frame.ndim != 3 or frame.shape[2] != 3:
        raise ValueError(f"fwdetect wants an H x W x 3 BGR array, got {getattr(frame, 'shape', type(frame))}")
    if frame.dtype != np.uint8:
        raise ValueError(f"fwdetect wants uint8 pixels, got {frame.dtype}")
    if frame.shape[1] < MIN_WIDTH:
        raise ValueError(f"fwdetect needs frames at least {MIN_WIDTH} px wide, got {frame.shape[1]}")


def _is_dropped_feed(frame: npt.NDArray[np.uint8]) -> bool:
    sample = frame[::8, ::8].astype(np.int16)
    return bool(abs(float(sample.mean()) - _POISON_GREY) < 12 and float(sample.std()) < 6)


def _cost(frame: npt.NDArray[np.uint8]) -> int:
    # Vectorized part (releases the GIL): a cheap blur, like a network's first layers.
    small = frame[::2, ::2].astype(np.float32)
    blurred = (small[:-1, :-1] + small[1:, 1:]) * 0.5
    acc = int(blurred.sum()) & 0xFFFF
    # Pure-Python part (holds the GIL), proportional to the pixel count.
    steps = frame.shape[0] * frame.shape[1] // _PIXELS_PER_STEP
    for i in range(steps):
        acc = (acc * 31 + i) & 0xFFFF
    return acc


def _find(frame: npt.NDArray[np.uint8], label: str) -> Box | None:
    mask = frame[:, :, _CHANNEL[label]] > _BRIGHT
    count = int(mask.sum())
    if count < _MIN_PIXELS:
        return None
    rows = np.flatnonzero(mask.any(axis=1))
    cols = np.flatnonzero(mask.any(axis=0))
    y0, y1 = int(rows[0]), int(rows[-1])
    x0, x1 = int(cols[0]), int(cols[-1])
    w, h = x1 - x0 + 1, y1 - y0 + 1
    return Box(label, x0, y0, w, h, round(count / (w * h), 3))


def _trace(frame: npt.NDArray[np.uint8], boxes: list[Box]) -> None:
    path = os.environ.get("FWDETECT_TRACE")
    if not path:
        return
    line = json.dumps(
        {
            "pid": os.getpid(),
            "t": time.time(),
            "w": frame.shape[1],
            "h": frame.shape[0],
            "boxes": [[b.label, b.x, b.y, b.w, b.h] for b in boxes],
        }
    )
    # One O_APPEND write per line, so lines from many processes never interleave.
    fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o644)
    try:
        os.write(fd, (line + "\n").encode())
    finally:
        os.close(fd)
