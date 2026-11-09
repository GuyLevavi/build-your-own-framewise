import argparse
import json

import cv2


def probe(path: str) -> dict[str, float | int]:
    capture = cv2.VideoCapture(path)
    try:
        width = int(capture.get(cv2.CAP_PROP_FRAME_WIDTH))
        height = int(capture.get(cv2.CAP_PROP_FRAME_HEIGHT))
        fps = float(capture.get(cv2.CAP_PROP_FPS))
        frame_count = int(capture.get(cv2.CAP_PROP_FRAME_COUNT))
    finally:
        capture.release()
    return {
        "width": width,
        "height": height,
        "fps": fps,
        "frame_count": frame_count,
        "duration_s": round(frame_count / fps, 3),
    }


def main() -> None:
    parser = argparse.ArgumentParser(prog="framewise")
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("probe")
    p.add_argument("video")
    args = parser.parse_args()
    print(json.dumps(probe(args.video)))


if __name__ == "__main__":
    main()
