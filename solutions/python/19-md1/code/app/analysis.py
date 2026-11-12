"""Running the model over a video, and turning sightings into segments."""

import signal
from collections.abc import Callable, Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from app.model import Box, run, use_threads
from app.profiles import Profile
from app.video import frames, open_video

Segment = dict[str, float]
Labels = dict[str, list[Segment]]


@dataclass(frozen=True)
class FrameResult:
    index: int
    t_s: float
    boxes: list[Box]


def detect(frame: np.ndarray, profile: Profile) -> list[Box]:
    """The profile's model on one frame, at the profile's input size, in the frame's pixels."""
    return run(profile.model, frame, profile.input_size)


class Interrupted(Exception):
    """The caller asked analysis to stop between frames."""


def detect_frames(
    path: Path,
    profile: Profile,
    should_stop: Callable[[], bool] = lambda: False,
    start: int = 0,
    stop: int | None = None,
) -> Iterator[FrameResult]:
    """Run the model on every sampled frame in [start, stop), one frame at a time."""
    with open_video(path) as (capture, info):
        for index, frame in frames(capture, profile.every, start, stop):
            if should_stop():
                raise Interrupted
            yield FrameResult(index, index / info.fps, detect(frame, profile))


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


def detect_chunk(chunk: Chunk) -> list[FrameResult]:
    """Runs in a pool process."""
    return list(detect_frames(chunk.path, chunk.profile, start=chunk.start, stop=chunk.stop))


def start_process(threads: int) -> None:
    """Runs first in every pool process.

    Ctrl-C reaches the whole process group: leave it to the parent, which stops the pool.
    SIGTERM keeps its default: pools stop their processes with it. And the model gets its
    share of the cores: each process using all of them only slows the others down.
    """
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    use_threads(threads)


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
