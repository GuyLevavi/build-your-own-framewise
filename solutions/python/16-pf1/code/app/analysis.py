"""Running the detector over a video, and turning sightings into segments."""

import dataclasses
from collections.abc import Callable, Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path

import cv2
import fwdetect
import numpy as np

from app.profiles import Profile
from app.video import frames, open_video

Segment = dict[str, float]
Labels = dict[str, list[Segment]]


@dataclass(frozen=True)
class FrameResult:
    index: int
    t_s: float
    boxes: list[fwdetect.Box]


# fwdetect's cost grows with the pixels it gets and its accuracy doesn't, down to
# its MIN_WIDTH: run it at this width and scale the boxes back.
DETECT_WIDTH = 640


def detect(frame: np.ndarray) -> list[fwdetect.Box]:
    """fwdetect.detect, at no more than DETECT_WIDTH, in the frame's own coordinates."""
    height, width = frame.shape[:2]
    if width <= DETECT_WIDTH:
        return fwdetect.detect(frame)
    scale = width / DETECT_WIDTH
    small = cv2.resize(frame, (DETECT_WIDTH, round(height / scale)), interpolation=cv2.INTER_AREA)
    return [
        dataclasses.replace(
            box,
            x=round(box.x * scale),
            y=round(box.y * scale),
            w=round(box.w * scale),
            h=round(box.h * scale),
        )
        for box in fwdetect.detect(small)
    ]


class Interrupted(Exception):
    """The caller asked analysis to stop between frames."""


def detect_frames(
    path: Path, profile: Profile, should_stop: Callable[[], bool] = lambda: False
) -> Iterator[FrameResult]:
    """Run the detector on every sampled frame, one frame at a time."""
    with open_video(path) as (capture, info):
        for index, frame in frames(capture, profile.every):
            if should_stop():
                raise Interrupted
            yield FrameResult(index, index / info.fps, detect(frame))


@dataclass
class _Run:
    start: float
    end: float


def presence(sightings: Iterable[tuple[float, set[str]]], profile: Profile) -> Labels:
    """Group the times each label was seen into segments, splitting on gaps over max_gap_s."""
    runs: dict[str, list[_Run]] = {}
    for t, labels in sightings:
        for label in labels & set(profile.labels):
            label_runs = runs.setdefault(label, [])
            if label_runs and t - label_runs[-1].end <= profile.max_gap_s + 1e-9:
                label_runs[-1].end = t
            else:
                label_runs.append(_Run(t, t))
    return {
        label: [{"start_s": round(r.start, 3), "end_s": round(r.end, 3)} for r in label_runs]
        for label, label_runs in sorted(runs.items())
    }


def analyze(path: Path, profile: Profile) -> Labels:
    results = detect_frames(path, profile)
    return presence(((r.t_s, {b.label for b in r.boxes}) for r in results), profile)
