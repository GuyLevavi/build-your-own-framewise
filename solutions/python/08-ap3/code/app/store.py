"""Where videos and jobs live: the repository the API talks to."""

import uuid
from dataclasses import dataclass
from enum import StrEnum
from typing import Any

import sqlalchemy as sa

from app import db
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
    """Videos and jobs in Postgres. One short transaction per operation."""

    def __init__(self, engine: sa.Engine) -> None:
        self.engine = engine

    def add_video(self, video: Video) -> None:
        with self.engine.begin() as conn:
            conn.execute(
                sa.insert(db.videos).values(
                    id=video.id,
                    filename=video.filename,
                    size_bytes=video.size_bytes,
                    path=video.path,
                )
            )

    def video(self, video_id: str) -> Video | None:
        with self.engine.connect() as conn:
            row = conn.execute(sa.select(db.videos).where(db.videos.c.id == video_id)).first()
        return Video(row.id, row.filename, row.size_bytes, row.path) if row else None

    def add_job(self, video_id: str, profile: str) -> Job:
        job = Job(new_id(), video_id, profile, Status.PENDING, None, None)
        with self.engine.begin() as conn:
            conn.execute(
                sa.insert(db.jobs).values(
                    id=job.id, video_id=video_id, profile=profile, status=job.status.value
                )
            )
        return job

    def job(self, job_id: str) -> Job | None:
        with self.engine.connect() as conn:
            row = conn.execute(sa.select(db.jobs).where(db.jobs.c.id == job_id)).first()
        if row is None:
            return None
        return Job(row.id, row.video_id, row.profile, Status(row.status), row.error, row.labels)

    def unfinished_jobs(self) -> list[str]:
        with self.engine.connect() as conn:
            rows = conn.execute(
                sa.select(db.jobs.c.id)
                .where(db.jobs.c.status.in_([Status.PENDING.value, Status.RUNNING.value]))
                .order_by(db.jobs.c.created_at)
            )
            return [row.id for row in rows]

    def set_status(
        self, job_id: str, status: Status, error: str | None = None, labels: Labels | None = None
    ) -> None:
        with self.engine.begin() as conn:
            conn.execute(
                sa.update(db.jobs)
                .where(db.jobs.c.id == job_id)
                .values(status=status.value, error=error, labels=labels, updated_at=sa.func.now())
            )
