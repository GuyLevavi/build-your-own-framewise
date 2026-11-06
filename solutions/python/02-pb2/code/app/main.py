import argparse
import json
from pathlib import Path

from app import video


def cmd_probe(args: argparse.Namespace) -> None:
    print(json.dumps(video.probe(args.video)))


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="framewise")
    commands = parser.add_subparsers(dest="command", required=True)

    probe = commands.add_parser("probe", help="print video metadata as JSON")
    probe.add_argument("video", type=Path)
    probe.set_defaults(func=cmd_probe)
    return parser


def main() -> None:
    args = build_parser().parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
