"""python -m fwdetect bench: single-core detector speed on this machine.

The course tester runs this before the performance stages and states its fps
targets as multiples of the number it prints.
"""

from __future__ import annotations

import argparse
import json
import sys
import time

import numpy as np

from fwdetect import detect


def _frame(width: int, height: int) -> np.ndarray:
    frame = np.zeros((height, width, 3), dtype=np.uint8)
    frame[height // 4 : height // 2, width // 8 : width // 4, 2] = 255  # a person
    frame[height // 2 : 3 * height // 4, width // 2 : 3 * width // 4, 0] = 255  # a car
    return frame


def bench(seconds: float, width: int, height: int) -> dict[str, float | int]:
    frame = _frame(width, height)
    detect(frame)  # warm up
    calls = 0
    start = time.perf_counter()
    while (elapsed := time.perf_counter() - start) < seconds:
        detect(frame)
        calls += 1
    return {"fps": round(calls / elapsed, 2), "width": width, "height": height, "calls": calls}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="python -m fwdetect")
    sub = parser.add_subparsers(dest="command", required=True)
    b = sub.add_parser("bench", help="measure single-core frames per second")
    b.add_argument("--seconds", type=float, default=2.0)
    b.add_argument("--width", type=int, default=640)
    b.add_argument("--height", type=int, default=360)
    args = parser.parse_args(argv)
    print(json.dumps(bench(args.seconds, args.width, args.height)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
