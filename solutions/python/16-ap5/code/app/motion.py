"""Find motion: compare each frame with the previous one, then group motion frames into segments."""

from collections.abc import Iterable
from pathlib import Path

import cv2

from app import video
from app.config import Detector


def motion_frames(path: Path, detector: Detector) -> list[int]:
    hits: list[int] = []
    previous = None
    for index, frame in video.frames(path):
        gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
        if previous is not None:
            changed = int((cv2.absdiff(gray, previous) > detector.threshold).sum())
            if changed >= detector.min_area:
                hits.append(index)
        previous = gray
    return hits


def segments(hits: Iterable[int], fps: float, max_gap_s: float) -> list[dict[str, float]]:
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


def analyze(path: Path, detector: Detector, max_gap_s: float) -> list[dict[str, float]]:
    hits = motion_frames(path, detector)
    return segments(hits, video.fps_of(path), max_gap_s)
