"""Read video files with OpenCV."""

from pathlib import Path

import cv2


def probe(path: Path) -> dict[str, float | int]:
    capture = cv2.VideoCapture(str(path))
    try:
        fps = capture.get(cv2.CAP_PROP_FPS)
        frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT))
        return {
            "width": int(capture.get(cv2.CAP_PROP_FRAME_WIDTH)),
            "height": int(capture.get(cv2.CAP_PROP_FRAME_HEIGHT)),
            "fps": fps,
            "frame_count": frame_count,
            "duration_s": frame_count / fps if fps else 0.0,
        }
    finally:
        capture.release()
