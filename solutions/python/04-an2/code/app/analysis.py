"""Turning detector output into when each label was present."""

from collections.abc import Iterable
from dataclasses import dataclass
from pathlib import Path

import fwdetect
import numpy as np

from app.profiles import Profile
from app.video import frames, open_video

Segment = dict[str, float]
Labels = dict[str, list[Segment]]


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


def detect_labels(frame: np.ndarray) -> set[str]:
    return {box.label for box in fwdetect.detect(frame)}


def analyze(path: Path, profile: Profile) -> Labels:
    with open_video(path) as (capture, info):
        sightings = (
            (index / info.fps, detect_labels(frame))
            for index, frame in frames(capture, profile.every)
        )
        return presence(sightings, profile)
