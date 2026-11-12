"""Running the model over a video, and turning sightings into segments."""

from collections.abc import Callable, Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path

from app.model import Box, detect
from app.profiles import Profile
from app.video import frames, open_video

Segment = dict[str, float]
Labels = dict[str, list[Segment]]


@dataclass(frozen=True)
class FrameResult:
    index: int
    t_s: float
    boxes: list[Box]


class Interrupted(Exception):
    """The caller asked analysis to stop between frames."""


def detect_frames(
    path: Path, profile: Profile, should_stop: Callable[[], bool] = lambda: False
) -> Iterator[FrameResult]:
    """Run the model on every sampled frame, one frame at a time."""
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
