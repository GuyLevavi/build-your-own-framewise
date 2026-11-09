import argparse
import json
import logging
import sys
from pathlib import Path

from app.errors import FramewiseError
from app.profiles import Profile, load_profiles, pick


def cmd_probe(args: argparse.Namespace) -> None:
    from app.video import probe

    print(json.dumps(probe(args.video).as_dict()))


def cmd_analyze(args: argparse.Namespace) -> None:
    from app.analysis import analyze

    if args.profile:
        profile = pick(load_profiles(args.config), args.profile, args.config)
    else:
        profile = Profile(every=args.every, max_gap_s=args.max_gap)
    print(json.dumps({"labels": analyze(args.video, profile)}))


def cmd_serve(args: argparse.Namespace) -> None:
    import uvicorn

    from app.server import Settings, create_app
    from app.store import Store

    app = create_app(Settings.from_env(args.data_dir), Store())
    uvicorn.run(app, host="127.0.0.1", port=args.port, log_level="warning")


def main() -> None:
    logging.basicConfig(level=logging.WARNING, stream=sys.stderr)
    parser = argparse.ArgumentParser(prog="framewise")
    sub = parser.add_subparsers(dest="command", required=True)

    p = sub.add_parser("probe", help="print a video's size, frame rate and length")
    p.add_argument("video", type=Path)
    p.set_defaults(run=cmd_probe)

    a = sub.add_parser("analyze", help="report when each label appears")
    a.add_argument("video", type=Path)
    a.add_argument("--every", type=int, default=1)
    a.add_argument("--max-gap", type=float, default=0.5)
    a.add_argument("--config", type=Path, default=Path("profiles.yaml"))
    a.add_argument("--profile")
    a.set_defaults(run=cmd_analyze)

    s = sub.add_parser("serve", help="run the HTTP API")
    s.add_argument("--port", type=int, default=8000)
    s.add_argument("--data-dir", type=Path, default=Path("data"))
    s.set_defaults(run=cmd_serve)

    args = parser.parse_args()
    try:
        args.run(args)
    except FramewiseError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
