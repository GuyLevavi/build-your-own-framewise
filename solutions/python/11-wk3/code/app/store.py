"""Where videos and jobs live: the repository the API and workers talk to."""

import uuid
from dataclasses import dataclass
from datetime import timedelta
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
    attempts: int = 0

    def public(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "video_id": self.video_id,
            "profile": self.profile,
            "status": self.status.value,
            "error": self.error,
            "labels": self.labels or {},
            "attempts": self.attempts,
        }


def new_id() -> str:
    return uuid.uuid4().hex


def _job(row: sa.Row) -> Job:
    return Job(
        row.id, row.video_id, row.profile, Status(row.status), row.error, row.labels, row.attempts
    )


class Store:
    """Videos and jobs in Postgres. One short transaction per operation."""

    def __init__(self, engine: sa.Engine) -> None:
        self.engine = engine

    # Videos

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

    # Jobs, as the API sees them

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
        return _job(row) if row else None

    # Jobs, as workers see them

    def claim(self, lease_s: float, max_attempts: int) -> Job | None:
        """Take the oldest job nobody holds: PENDING, or RUNNING with a lapsed lease.

        SKIP LOCKED makes concurrent workers pass over a row another one is
        claiming instead of queueing behind it. A job that has used up its
        attempts is failed here, since its worker died each time.
        """
        jobs = db.jobs.c
        while True:
            with self.engine.begin() as conn:
                row = conn.execute(
                    sa.select(db.jobs)
                    .where(
                        sa.or_(
                            jobs.status == Status.PENDING.value,
                            sa.and_(
                                jobs.status == Status.RUNNING.value,
                                jobs.lease_until < sa.func.now(),
                            ),
                        )
                    )
                    .order_by(jobs.created_at)
                    .limit(1)
                    .with_for_update(skip_locked=True)
                ).first()
                if row is None:
                    return None
                if row.attempts >= max_attempts:
                    conn.execute(
                        sa.update(db.jobs)
                        .where(jobs.id == row.id)
                        .values(
                            status=Status.FAILED.value,
                            error=f"gave up after {row.attempts} attempts: the worker died each time",
                            lease_until=None,
                            updated_at=sa.func.now(),
                        )
                    )
                    continue
                conn.execute(
                    sa.update(db.jobs)
                    .where(jobs.id == row.id)
                    .values(
                        status=Status.RUNNING.value,
                        attempts=jobs.attempts + 1,
                        lease_until=sa.func.now() + timedelta(seconds=lease_s),
                        updated_at=sa.func.now(),
                    )
                )
                return Job(
                    row.id, row.video_id, row.profile, Status.RUNNING, None, None, row.attempts + 1
                )

    def renew(self, job_id: str, lease_s: float) -> bool:
        """Extend our lease. False means the job is no longer ours."""
        with self.engine.begin() as conn:
            result = conn.execute(
                sa.update(db.jobs)
                .where(db.jobs.c.id == job_id, db.jobs.c.status == Status.RUNNING.value)
                .values(lease_until=sa.func.now() + timedelta(seconds=lease_s))
            )
        return result.rowcount == 1

    def finish(
        self, job_id: str, status: Status, error: str | None = None, labels: Labels | None = None
    ) -> None:
        with self.engine.begin() as conn:
            conn.execute(
                sa.update(db.jobs)
                .where(db.jobs.c.id == job_id)
                .values(
                    status=status.value,
                    error=error,
                    labels=labels,
                    lease_until=None,
                    updated_at=sa.func.now(),
                )
            )
