"""Runs jobs: one background thread fed by a queue of job ids."""

import logging
import queue
import threading
from pathlib import Path

from app.analysis import analyze
from app.errors import FramewiseError
from app.profiles import Profile
from app.store import Status, Store

log = logging.getLogger(__name__)


class Worker:
    def __init__(self, store: Store, profiles: dict[str, Profile]) -> None:
        self.store = store
        self.profiles = profiles
        self.queue: queue.Queue[str] = queue.Queue()
        self.thread = threading.Thread(target=self._loop, name="worker", daemon=True)

    def start(self) -> None:
        # Jobs left PENDING or RUNNING by the last run were interrupted, not done.
        for job_id in self.store.unfinished_jobs():
            self.queue.put(job_id)
        self.thread.start()

    def submit(self, job_id: str) -> None:
        self.queue.put(job_id)

    def _loop(self) -> None:
        while True:
            self.run(self.queue.get())

    def run(self, job_id: str) -> None:
        job = self.store.job(job_id)
        video = self.store.video(job.video_id) if job else None
        if job is None or video is None:
            return
        self.store.set_status(job_id, Status.RUNNING)
        try:
            labels = analyze(Path(video.path), self.profiles[job.profile])
        except FramewiseError as e:
            self.store.set_status(job_id, Status.FAILED, error=str(e))
        except Exception as e:  # a bug, not bad input: keep the service up, record it
            log.exception("job %s crashed", job_id)
            self.store.set_status(job_id, Status.FAILED, error=f"internal error: {e}")
        else:
            self.store.set_status(job_id, Status.DONE, labels=labels)
