import argparse
import json
import sys
from pathlib import Path

from app.errors import FramewiseError
from app.profiles import Profile


def cmd_probe(args: argparse.Namespace) -> None:
    from app.video import probe

    print(json.dumps(probe(args.video).as_dict()))


def cmd_analyze(args: argparse.Namespace) -> None:
    from app.analysis import analyze

    profile = Profile(every=args.every, max_gap_s=args.max_gap)
    print(json.dumps({"labels": analyze(args.video, profile)}))


def main() -> None:
    parser = argparse.ArgumentParser(prog="framewise")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("probe", help="print a video's size, frame rate and length")
    p.add_argument("video", type=Path)
    p.set_defaults(run=cmd_probe)

    a = sub.add_parser("analyze", help="report when each label appears")
    a.add_argument("video", type=Path)
    a.add_argument("--every", type=int, default=1)
    a.add_argument("--max-gap", type=float, default=0.5)
    a.set_defaults(run=cmd_analyze)

    args = parser.parse_args()
    try:
        args.run(args)
    except FramewiseError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
