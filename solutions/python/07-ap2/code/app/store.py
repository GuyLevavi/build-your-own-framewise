"""Where videos and jobs live: the repository the API talks to."""

import threading
import uuid
from dataclasses import dataclass, replace
from enum import StrEnum
from typing import Any

from app.analysis import Labels


class Status(StrEnum):
    PENDING = "PENDING"
    RUNNING = "RUNNING"
    DONE = "DONE"
    FAILED = "FAILED"


@dataclass(frozen=True)
class Video:
    id: str
    filename: str
    size_bytes: int
    path: str

    def public(self) -> dict[str, Any]:
        return {"video_id": self.id, "filename": self.filename, "size_bytes": self.size_bytes}


@dataclass(frozen=True)
class Job:
    id: str
    video_id: str
    profile: str
    status: Status
    error: str | None
    labels: Labels | None

    def public(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "video_id": self.video_id,
            "profile": self.profile,
            "status": self.status.value,
            "error": self.error,
            "labels": self.labels or {},
        }


def new_id() -> str:
    return uuid.uuid4().hex


class Store:
    """Videos and jobs in memory, behind one lock: the worker thread and the
    request handlers share them, and nobody may see a half-updated job."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._videos: dict[str, Video] = {}
        self._jobs: dict[str, Job] = {}

    def add_video(self, video: Video) -> None:
        with self._lock:
            self._videos[video.id] = video

    def video(self, video_id: str) -> Video | None:
        with self._lock:
            return self._videos.get(video_id)

    def add_job(self, video_id: str, profile: str) -> Job:
        job = Job(new_id(), video_id, profile, Status.PENDING, None, None)
        with self._lock:
            self._jobs[job.id] = job
        return job

    def job(self, job_id: str) -> Job | None:
        with self._lock:
            return self._jobs.get(job_id)

    def unfinished_jobs(self) -> list[str]:
        with self._lock:
            return [
                j.id for j in self._jobs.values() if j.status in (Status.PENDING, Status.RUNNING)
            ]

    def set_status(
        self, job_id: str, status: Status, error: str | None = None, labels: Labels | None = None
    ) -> None:
        with self._lock:
            self._jobs[job_id] = replace(
                self._jobs[job_id], status=status, error=error, labels=labels
            )
