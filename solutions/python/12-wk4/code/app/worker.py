"""`work`: a process that claims jobs from Postgres and runs them, one at a time.

Run as many as you like. Each claim comes with a lease the worker keeps
renewing while it works; a worker that dies stops renewing, its lease runs
out, and another worker takes the job over.
"""

import logging
import os
import threading
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Self

from app.analysis import Interrupted, Labels, detect_frames, presence
from app.errors import FramewiseError
from app.profiles import Profile
from app.store import Job, Status, Store

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class WorkerSettings:
    lease_s: float = 30.0
    max_attempts: int = 3
    poll_s: float = 0.5

    @classmethod
    def from_env(cls) -> "WorkerSettings":
        return cls(
            lease_s=float(os.environ.get("FRAMEWISE_LEASE_S", "30")),
            max_attempts=int(os.environ.get("FRAMEWISE_MAX_ATTEMPTS", "3")),
        )


class Heartbeat:
    """Renews a job's lease in the background until stopped; notices if it was lost."""

    def __init__(self, store: Store, job_id: str, lease_s: float) -> None:
        self.store, self.job_id, self.lease_s = store, job_id, lease_s
        self.lost = threading.Event()
        self._done = threading.Event()
        self._thread = threading.Thread(target=self._beat, name="heartbeat", daemon=True)

    def __enter__(self) -> Self:
        self._thread.start()
        return self

    def __exit__(self, *_: object) -> None:
        self._done.set()
        self._thread.join()

    def _beat(self) -> None:
        while not self._done.wait(self.lease_s / 3):
            try:
                if not self.store.renew(self.job_id, self.lease_s):
                    self.lost.set()
                    return
            except Exception:  # the database blinked: try again next beat
                log.warning("couldn't renew the lease on job %s", self.job_id, exc_info=True)


class Worker:
    def __init__(
        self, store: Store, profiles: dict[str, Profile], settings: WorkerSettings
    ) -> None:
        self.store = store
        self.profiles = profiles
        self.settings = settings
        self.stop = threading.Event()

    def run_forever(self) -> None:
        while not self.stop.is_set():
            job = self.store.claim(self.settings.lease_s, self.settings.max_attempts)
            if job is None:
                self.stop.wait(self.settings.poll_s)
                continue
            self.run(job)

    def run(self, job: Job) -> None:
        video = self.store.video(job.video_id)
        if video is None or job.profile not in self.profiles:
            self.store.finish(job.id, Status.FAILED, error="its video or profile is gone")
            return
        with Heartbeat(self.store, job.id, self.settings.lease_s) as heartbeat:
            try:
                labels = self._analyze(
                    job, Path(video.path), lambda: self.stop.is_set() or heartbeat.lost.is_set()
                )
            except Interrupted:
                if heartbeat.lost.is_set():
                    log.warning("lost the lease on job %s; leaving it to its new worker", job.id)
                else:
                    self.store.release(job.id)
                return
            except FramewiseError as e:
                self.store.finish(job.id, Status.FAILED, error=str(e))
            except Exception as e:  # a bug, not bad input: keep the worker up, record it
                log.exception("job %s crashed", job.id)
                self.store.finish(job.id, Status.FAILED, error=f"internal error: {e}")
            else:
                self.store.finish(job.id, Status.DONE, labels=labels)

    def _analyze(self, job: Job, path: Path, should_stop: Callable[[], bool]) -> Labels:
        profile = self.profiles[job.profile]
        results = detect_frames(path, profile, should_stop)
        return presence(((r.t_s, {b.label for b in r.boxes}) for r in results), profile)
