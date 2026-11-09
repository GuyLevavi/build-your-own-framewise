"""Reading video files: their facts, and their frames one at a time."""

from collections.abc import Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path

import cv2
import numpy as np

from app.errors import FramewiseError


class VideoError(FramewiseError):
    pass


@dataclass(frozen=True)
class VideoInfo:
    width: int
    height: int
    fps: float
    frame_count: int

    @property
    def duration_s(self) -> float:
        return self.frame_count / self.fps

    def as_dict(self) -> dict[str, float | int]:
        return {
            "width": self.width,
            "height": self.height,
            "fps": self.fps,
            "frame_count": self.frame_count,
            "duration_s": round(self.duration_s, 3),
        }


@contextmanager
def open_video(path: Path) -> Iterator[tuple[cv2.VideoCapture, VideoInfo]]:
    if not path.is_file():
        raise VideoError(f"{path}: no such file")
    capture = cv2.VideoCapture(str(path))
    try:
        info = VideoInfo(
            width=int(capture.get(cv2.CAP_PROP_FRAME_WIDTH)),
            height=int(capture.get(cv2.CAP_PROP_FRAME_HEIGHT)),
            fps=float(capture.get(cv2.CAP_PROP_FPS)),
            frame_count=int(capture.get(cv2.CAP_PROP_FRAME_COUNT)),
        )
        if not capture.isOpened() or info.width <= 0 or info.fps <= 0 or info.frame_count <= 0:
            raise VideoError(f"{path}: not a playable video")
        # A header alone isn't a video: the first frame must decode too.
        if not capture.read()[0]:
            raise VideoError(f"{path}: not a playable video (no frame decodes)")
        capture.set(cv2.CAP_PROP_POS_FRAMES, 0)
        yield capture, info
    finally:
        capture.release()


def probe(path: Path) -> VideoInfo:
    with open_video(path) as (_, info):
        return info


def frames(capture: cv2.VideoCapture, every: int) -> list[tuple[int, np.ndarray]]:
    """Every `every`-th frame with its index."""
    decoded = []
    while True:
        ok, frame = capture.read()
        if not ok:
            break
        decoded.append(frame)
    return [(index, decoded[index]) for index in range(0, len(decoded), every)]
