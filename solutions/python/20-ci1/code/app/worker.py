"""`work`: a process that claims jobs from Postgres and runs them, one at a time.

Run as many as you like. Each claim comes with a lease the worker keeps
renewing while it works; a worker that dies stops renewing, its lease runs
out, and another worker takes the job over.
"""

import logging
import os
import threading
import time
from collections.abc import Callable, Iterator
from dataclasses import dataclass
from pathlib import Path
from typing import Self

from app.analysis import FrameResult, Interrupted, Labels, detect_frames, presence
from app.errors import FramewiseError
from app.profiles import Profile
from app.remote import ModelClient, ModelError, remote_frames
from app.store import Detection, Job, Status, Store

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class WorkerSettings:
    lease_s: float = 30.0
    max_attempts: int = 3
    poll_s: float = 0.5
    batch_frames: int = 50
    model_url: str | None = None
    model_timeout_s: float = 10.0

    @classmethod
    def from_env(cls) -> "WorkerSettings":
        return cls(
            lease_s=float(os.environ.get("FRAMEWISE_LEASE_S", "30")),
            max_attempts=int(os.environ.get("FRAMEWISE_MAX_ATTEMPTS", "3")),
            model_url=os.environ.get("FRAMEWISE_MODEL_URL") or None,
            model_timeout_s=float(os.environ.get("FRAMEWISE_MODEL_TIMEOUT_S", "10")),
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
                log.warning(
                    "couldn't renew the lease", extra={"job_id": self.job_id}, exc_info=True
                )


class Worker:
    def __init__(
        self, store: Store, profiles: dict[str, Profile], settings: WorkerSettings
    ) -> None:
        self.store = store
        self.profiles = profiles
        self.settings = settings
        self.stop = threading.Event()
        self._model: ModelClient | None = None

    def model(self) -> ModelClient:
        """The model server's client: made once, it remembers each model's contract."""
        if self._model is None:
            if not self.settings.model_url:
                raise ModelError(
                    "the profile's detector is remote, and FRAMEWISE_MODEL_URL isn't set"
                )
            self._model = ModelClient(self.settings.model_url, self.settings.model_timeout_s)
        return self._model

    def run_forever(self) -> None:
        while not self.stop.is_set():
            job = self.store.claim(self.settings.lease_s, self.settings.max_attempts)
            if job is None:
                self.stop.wait(self.settings.poll_s)
                continue
            self.run(job)

    def run(self, job: Job) -> None:
        context = {"job_id": job.id, "attempt": job.attempts}
        log.info("took job", extra={**context, "video_id": job.video_id, "profile": job.profile})
        started = time.perf_counter()
        video = self.store.video(job.video_id)
        if video is None or job.profile not in self.profiles:
            self.store.finish(job.id, Status.FAILED, error="its video or profile is gone")
            return
        with Heartbeat(self.store, job.id, self.settings.lease_s) as heartbeat:
            try:
                labels, frames = self._analyze(
                    job, Path(video.path), lambda: self.stop.is_set() or heartbeat.lost.is_set()
                )
            except Interrupted:
                if heartbeat.lost.is_set():
                    log.warning("lost the lease; leaving the job to its new worker", extra=context)
                else:
                    self.store.release(job.id)
                return
            except FramewiseError as e:
                self.store.finish(job.id, Status.FAILED, error=str(e))
                log.info("job failed", extra={**context, "error": str(e)})
            except Exception as e:  # a bug, not bad input: keep the worker up, record it
                log.exception("job crashed", extra=context)
                self.store.finish(job.id, Status.FAILED, error=f"internal error: {e}")
            else:
                seconds = time.perf_counter() - started
                self.store.finish(
                    job.id, Status.DONE, labels=labels, frames=frames, seconds=seconds
                )
                log.info(
                    "finished job",
                    extra={
                        **context,
                        "frames": frames,
                        "seconds": round(seconds, 3),
                        "fps": round(frames / seconds, 1),
                    },
                )

    def _analyze(self, job: Job, path: Path, should_stop: Callable[[], bool]) -> tuple[Labels, int]:
        profile = self.profiles[job.profile]
        sightings: list[tuple[float, set[str]]] = []
        batch: list[Detection] = []
        for frame_number, result in enumerate(self._results(path, profile, should_stop), 1):
            boxes = [b for b in result.boxes if b.label in profile.labels]
            sightings.append((result.t_s, {b.label for b in boxes}))
            batch += [
                Detection(result.index, result.t_s, b.label, b.x, b.y, b.w, b.h, b.score)
                for b in boxes
            ]
            if frame_number % self.settings.batch_frames == 0:
                self.store.add_detections(job.id, batch)
                batch = []
        self.store.add_detections(job.id, batch)
        return presence(sightings, profile), len(sightings)

    def _results(
        self, path: Path, profile: Profile, should_stop: Callable[[], bool]
    ) -> Iterator[FrameResult]:
        """Every sampled frame's result, in order: from the model server, or from this process."""
        if profile.detector == "remote":
            return remote_frames(path, profile, self.model(), should_stop)
        return detect_frames(path, profile, should_stop)
