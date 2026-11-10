"""`work`: a process that claims jobs from Postgres and runs them, one at a time.

It runs beside the API, not inside it: the API only records jobs.
"""

import logging
import threading
from dataclasses import dataclass
from pathlib import Path

from app.analysis import analyze
from app.errors import FramewiseError
from app.profiles import Profile
from app.store import Job, Status, Store

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class WorkerSettings:
    poll_s: float = 0.5

    @classmethod
    def from_env(cls) -> "WorkerSettings":
        return cls()


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
            job = self.store.claim()
            if job is None:
                self.stop.wait(self.settings.poll_s)
                continue
            self.run(job)

    def run(self, job: Job) -> None:
        video = self.store.video(job.video_id)
        if video is None or job.profile not in self.profiles:
            self.store.finish(job.id, Status.FAILED, error="its video or profile is gone")
            return
        try:
            labels = analyze(Path(video.path), self.profiles[job.profile])
        except FramewiseError as e:
            self.store.finish(job.id, Status.FAILED, error=str(e))
        except Exception as e:  # a bug, not bad input: keep the worker up, record it
            log.exception("job %s crashed", job.id)
            self.store.finish(job.id, Status.FAILED, error=f"internal error: {e}")
        else:
            self.store.finish(job.id, Status.DONE, labels=labels)
