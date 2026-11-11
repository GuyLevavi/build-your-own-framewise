"""Running the detector over a video, and turning sightings into segments."""

import dataclasses
import signal
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
    path: Path,
    profile: Profile,
    should_stop: Callable[[], bool] = lambda: False,
    start: int = 0,
    stop: int | None = None,
) -> Iterator[FrameResult]:
    """Run the detector on every sampled frame in [start, stop), one frame at a time."""
    with open_video(path) as (capture, info):
        for index, frame in frames(capture, profile.every, start, stop):
            if should_stop():
                raise Interrupted
            yield FrameResult(index, index / info.fps, detect(frame))


# Splitting a video across processes: each process opens the file itself and
# decodes only its own range, so neither decoding nor frames have to go through
# the parent. Chunks are just (path, range): cheap to send, and small results back.


@dataclass(frozen=True)
class Chunk:
    path: Path
    profile: Profile
    start: int
    stop: int


def split(path: Path, profile: Profile, frames_per_chunk: int) -> list[Chunk]:
    with open_video(path) as (_, info):
        count = info.frame_count
    return [
        Chunk(path, profile, start, min(start + frames_per_chunk, count))
        for start in range(0, count, frames_per_chunk)
    ]


def sampled_frames(path: Path, profile: Profile) -> int:
    with open_video(path) as (_, info):
        return -(-info.frame_count // profile.every)


def detect_chunk(chunk: Chunk) -> list[FrameResult]:
    """Runs in a pool process."""
    return list(detect_frames(chunk.path, chunk.profile, start=chunk.start, stop=chunk.stop))


def ignore_interrupts() -> None:
    """Ctrl-C reaches the whole process group: leave it to the parent, which stops the pool.

    SIGTERM keeps its default: pools stop their processes with it.
    """
    signal.signal(signal.SIGINT, signal.SIG_IGN)


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
