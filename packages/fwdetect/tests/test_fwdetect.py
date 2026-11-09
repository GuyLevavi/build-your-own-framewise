"""The contract fwdetect's docstring promises, checked with frames built in NumPy.

test_scenes at the bottom also runs it on the tester's own clips through
OpenCV, when FWDETECT_SCENES points at a directory of them (see the README).
"""

from __future__ import annotations

import json
import os
import signal
import subprocess
import sys
import threading
import time
from pathlib import Path

import numpy as np
import pytest

from fwdetect import MIN_WIDTH, Box, detect
from fwdetect.__main__ import main as cli_main


def blank(width: int = 640, height: int = 360, level: int = 0) -> np.ndarray:
    return np.full((height, width, 3), level, dtype=np.uint8)


def paint(frame: np.ndarray, channel: int, x: int, y: int, w: int, h: int) -> None:
    frame[y : y + h, x : x + w, channel] = 255


def boxes_by_label(boxes: list[Box]) -> dict[str, tuple[int, int, int, int]]:
    return {b.label: (b.x, b.y, b.w, b.h) for b in boxes}


def test_finds_each_label_by_its_colour() -> None:
    frame = blank()
    paint(frame, 2, 20, 200, 40, 100)  # red: person
    paint(frame, 0, 300, 50, 160, 80)  # blue: car
    paint(frame, 1, 500, 10, 30, 30)  # green: bicycle
    assert boxes_by_label(detect(frame)) == {
        "person": (20, 200, 40, 100),
        "car": (300, 50, 160, 80),
        "bicycle": (500, 10, 30, 30),
    }


def test_overlapping_objects_keep_their_whole_boxes() -> None:
    frame = blank()
    paint(frame, 2, 100, 100, 40, 100)
    paint(frame, 0, 120, 150, 160, 80)  # overlaps the person: those pixels are magenta
    assert boxes_by_label(detect(frame)) == {"person": (100, 100, 40, 100), "car": (120, 150, 160, 80)}


def test_empty_and_noisy_frames_have_no_boxes() -> None:
    assert detect(blank()) == []
    rng = np.random.default_rng(1)
    noisy = (40 + rng.integers(-8, 9, size=(360, 640, 3))).astype(np.uint8)
    assert detect(noisy) == []


@pytest.mark.parametrize(
    "frame",
    [
        np.zeros((360, MIN_WIDTH - 1, 3), dtype=np.uint8),
        np.zeros((360, 640), dtype=np.uint8),
        np.zeros((360, 640, 3), dtype=np.float32),
    ],
    ids=["too narrow", "grey", "float"],
)
def test_rejects_frames_it_cannot_read(frame: np.ndarray) -> None:
    with pytest.raises(ValueError):
        detect(frame)


def test_a_dropped_feed_kills_the_process() -> None:
    code = "import numpy as np, fwdetect; fwdetect.detect(np.full((360, 640, 3), 128, np.uint8))"
    result = subprocess.run([sys.executable, "-c", code], capture_output=True, check=False)
    assert result.returncode == -signal.SIGABRT


def test_trace_writes_one_line_per_call(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    trace = tmp_path / "trace.jsonl"
    monkeypatch.setenv("FWDETECT_TRACE", str(trace))
    frame = blank()
    paint(frame, 2, 20, 200, 40, 100)
    detect(frame)
    detect(blank())
    lines = [json.loads(line) for line in trace.read_text().splitlines()]
    assert [line["pid"] for line in lines] == [os.getpid()] * 2
    assert lines[0]["boxes"] == [["person", 20, 200, 40, 100]]
    assert lines[1]["boxes"] == []
    assert (lines[0]["w"], lines[0]["h"]) == (640, 360)


def calls_per_second(frame: np.ndarray, seconds: float = 0.5) -> float:
    calls, start = 0, time.perf_counter()
    while time.perf_counter() - start < seconds:
        detect(frame)
        calls += 1
    return calls / (time.perf_counter() - start)


def test_cost_grows_with_pixels() -> None:
    # 4x the pixels should cost well over 2x: downscaling is a real optimization.
    assert calls_per_second(blank(640, 360)) > 2.5 * calls_per_second(blank(1280, 720))


def test_threads_do_not_help() -> None:
    # Most of the cost holds the GIL, so two threads barely beat one: pf2 needs processes.
    frame = blank()
    single = calls_per_second(frame, 1.0)
    counts = [0, 0]

    def run(k: int) -> None:
        end = time.perf_counter() + 1.0
        while time.perf_counter() < end:
            detect(frame)
            counts[k] += 1

    threads = [threading.Thread(target=run, args=(k,)) for k in range(2)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    assert sum(counts) < 1.4 * single


def test_bench_prints_fps(capsys: pytest.CaptureFixture[str]) -> None:
    assert cli_main(["bench", "--seconds", "0.3"]) == 0
    result = json.loads(capsys.readouterr().out)
    assert result["fps"] > 0 and (result["width"], result["height"]) == (640, 360)


# Presence segments the tester's clips_test.go quotes for the same clips, at
# every frame with gaps up to 0.5 s; checked here through OpenCV's decoder.
SCENES = {
    "street.avi": {"person": [(1.0, 8.0)], "car": [(4.0, 11.0)]},
    "noisy_street.avi": {"person": [(1.0, 8.0)], "car": [(4.0, 11.0)]},
    "poison.avi": {"person": [(0.0, 2.9)]},  # the poison frame (30) ends the run
}


@pytest.mark.skipif(not os.environ.get("FWDETECT_SCENES"), reason="set FWDETECT_SCENES to the tester's dumped scenes")
@pytest.mark.parametrize("name", sorted(SCENES))
def test_scenes(name: str) -> None:
    cv2 = pytest.importorskip("cv2")
    path = Path(os.environ["FWDETECT_SCENES"]) / name
    code = f"""
import json, cv2, fwdetect
cap = cv2.VideoCapture({str(path)!r})
fps = cap.get(cv2.CAP_PROP_FPS)
i = 0
while True:
    ok, frame = cap.read()
    if not ok:
        break
    labels = [b.label for b in fwdetect.detect(frame)]
    print(json.dumps([i / fps, labels]), flush=True)
    i += 1
"""
    del cv2
    result = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True, check=False)
    seen: dict[str, list[float]] = {}
    for line in result.stdout.splitlines():
        t, labels = json.loads(line)
        for label in labels:
            seen.setdefault(label, []).append(t)
    segments: dict[str, list[tuple[float, float]]] = {}
    for label, times in seen.items():
        runs: list[list[float]] = []
        for t in times:
            if runs and t - runs[-1][1] <= 0.5 + 1e-9:
                runs[-1][1] = t
            else:
                runs.append([t, t])
        segments[label] = [(round(a, 2), round(b, 2)) for a, b in runs]
    assert segments == SCENES[name]
    if name == "poison.avi":
        assert result.returncode == -signal.SIGABRT
