import argparse
import json
import os
import signal
import sys
from pathlib import Path

from app import logs
from app.errors import FramewiseError
from app.events import EventRule
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

    from app.db import make_engine
    from app.server import Settings, create_app
    from app.store import Store

    app = create_app(
        Settings.from_env(args.data_dir), Store(make_engine()), service_profiles(), service_events()
    )
    uvicorn.run(app, host=args.host, port=args.port, log_level="warning")


def cmd_work(args: argparse.Namespace) -> None:
    from app.db import make_engine
    from app.store import Store
    from app.worker import Worker, WorkerSettings

    worker = Worker(
        Store(make_engine(), service_events()), service_profiles(), WorkerSettings.from_env()
    )
    # SIGTERM (a deploy, a scale-down) asks us to stop: finish the frame, give the job back, exit 0.
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, lambda *_: worker.stop.set())
    try:
        worker.run_forever()
    finally:
        worker.close()


def service_profiles() -> dict[str, Profile]:
    return load_profiles(Path(os.environ.get("FRAMEWISE_PROFILES", "profiles.yaml")))


def service_events() -> dict[str, EventRule]:
    from app.events import load_events

    path = os.environ.get("FRAMEWISE_EVENTS")
    return load_events(Path(path)) if path else {}


def cmd_migrate(args: argparse.Namespace) -> None:
    from app.db import migrate

    migrate()


def main() -> None:
    logs.setup()
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
    # 127.0.0.1 on a workstation; in a pod or a container with its own network, 0.0.0.0, or
    # traffic from outside (a published port, a Service) never reaches the server.
    s.add_argument("--host", default="127.0.0.1")
    s.add_argument("--port", type=int, default=8000)
    s.add_argument("--data-dir", type=Path, default=Path("data"))
    s.set_defaults(run=cmd_serve)

    w = sub.add_parser("work", help="run jobs from the queue until stopped")
    w.set_defaults(run=cmd_work)

    m = sub.add_parser("migrate", help="create or upgrade the database schema")
    m.set_defaults(run=cmd_migrate)

    args = parser.parse_args()
    try:
        args.run(args)
    except FramewiseError as e:
        print(f"error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
