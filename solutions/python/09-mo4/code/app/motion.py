"""Find motion: compare each frame with the previous one, then group motion frames into segments."""

from collections.abc import Iterable
from pathlib import Path

import cv2

from app import video

THRESHOLD = 25  # a pixel changed when its grayscale difference is above this
MIN_AREA = 20  # a frame has motion when at least this many pixels changed
MAX_GAP_S = 0.5  # motion frames closer than this belong to one segment


def motion_frames(path: Path) -> list[int]:
    hits: list[int] = []
    previous = None
    for index, frame in video.frames(path):
        gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
        if previous is not None:
            changed = int((cv2.absdiff(gray, previous) > THRESHOLD).sum())
            if changed >= MIN_AREA:
                hits.append(index)
        previous = gray
    return hits


def segments(hits: Iterable[int], fps: float, max_gap_s: float) -> list[dict[str, float]]:
    """Group motion frames that are at most max_gap_s apart."""
    result: list[dict[str, float]] = []
    start = last = None
    for index in hits:
        if last is not None and (index - last) / fps > max_gap_s:
            result.append({"start_s": start / fps, "end_s": last / fps})
            start = None
        if start is None:
            start = index
        last = index
    if start is not None and last is not None:
        result.append({"start_s": start / fps, "end_s": last / fps})
    return result


def analyze(path: Path) -> list[dict[str, float]]:
    return segments(motion_frames(path), video.fps_of(path), MAX_GAP_S)
