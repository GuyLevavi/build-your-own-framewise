import argparse
import json
import sys
from pathlib import Path

from app.errors import FramewiseError
from app.video import probe


def main() -> None:
    parser = argparse.ArgumentParser(prog="framewise")
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("probe", help="print a video's size, frame rate and length")
    p.add_argument("video", type=Path)
    args = parser.parse_args()
    try:
        print(json.dumps(probe(args.video).as_dict()))
    except FramewiseError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
