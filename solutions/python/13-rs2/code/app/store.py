"""Where videos, jobs and detections live: the repository the API and workers talk to."""

import uuid
from collections.abc import Iterable
from dataclasses import dataclass
from datetime import timedelta
from enum import StrEnum
from typing import Any

import sqlalchemy as sa

from app import db
from app.analysis import Labels
from app.events import EventRule


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


@dataclass(frozen=True)
class Detection:
    frame: int
    t_s: float
    label: str
    x: int
    y: int
    w: int
    h: int
    score: float

    def public(self) -> dict[str, Any]:
        return {
            "t_s": self.t_s,
            "label": self.label,
            "x": self.x,
            "y": self.y,
            "w": self.w,
            "h": self.h,
        }


def new_id() -> str:
    return uuid.uuid4().hex


def _job(row: sa.Row) -> Job:
    return Job(
        row.id, row.video_id, row.profile, Status(row.status), row.error, row.labels, row.attempts
    )


# Overlapping (label_a, label_b) frames, grouped into islands no more than :gap
# apart, kept when they last at least :min_dur. The classic gaps-and-islands:
# mark where a new island starts, then a running sum numbers the islands.
EVENTS_SQL = sa.text("""
WITH pairs AS (
    SELECT DISTINCT a.frame, a.t_s
    FROM results.detections a
    JOIN results.detections b ON b.job_id = a.job_id AND b.frame = a.frame
    WHERE a.job_id = :job AND a.label = :label_a AND b.label = :label_b
      AND a.x < b.x + b.w AND b.x < a.x + a.w
      AND a.y < b.y + b.h AND b.y < a.y + a.h
),
marked AS (
    SELECT t_s,
           CASE WHEN t_s - lag(t_s) OVER (ORDER BY t_s) <= :gap + 1e-9 THEN 0 ELSE 1 END AS starts
    FROM pairs
),
islands AS (
    SELECT t_s, sum(starts) OVER (ORDER BY t_s) AS island FROM marked
)
SELECT min(t_s) AS start_s, max(t_s) AS end_s
FROM islands
GROUP BY island
HAVING max(t_s) - min(t_s) >= :min_dur - 1e-9
ORDER BY start_s
""")


class Store:
    """Videos, jobs and detections in Postgres. One short transaction per operation."""

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

    def release(self, job_id: str) -> None:
        """Give a job back unfinished. Stopping politely doesn't use up an attempt."""
        with self.engine.begin() as conn:
            conn.execute(
                sa.update(db.jobs)
                .where(db.jobs.c.id == job_id, db.jobs.c.status == Status.RUNNING.value)
                .values(
                    status=Status.PENDING.value,
                    attempts=db.jobs.c.attempts - 1,
                    lease_until=None,
                    updated_at=sa.func.now(),
                )
            )

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

    # Detections

    def add_detections(self, job_id: str, detections: Iterable[Detection]) -> None:
        rows = [{"job_id": job_id, **d.__dict__} for d in detections]
        if rows:
            with self.engine.begin() as conn:
                conn.execute(sa.insert(db.detections), rows)

    def detections(self, job_id: str, from_s: float | None, to_s: float | None) -> list[Detection]:
        d = db.detections.c
        query = sa.select(d.frame, d.t_s, d.label, d.x, d.y, d.w, d.h, d.score).where(
            d.job_id == job_id
        )
        if from_s is not None:
            query = query.where(d.t_s >= from_s - 1e-9)
        if to_s is not None:
            query = query.where(d.t_s <= to_s + 1e-9)
        with self.engine.connect() as conn:
            rows = conn.execute(query.order_by(d.t_s, d.label))
            return [Detection(*row) for row in rows]

    def events(self, job_id: str, rules: dict[str, EventRule]) -> list[dict[str, Any]]:
        found = []
        with self.engine.connect() as conn:
            for name, rule in rules.items():
                label_a, label_b = rule.labels
                rows = conn.execute(
                    EVENTS_SQL,
                    {
                        "job": job_id,
                        "label_a": label_a,
                        "label_b": label_b,
                        "gap": rule.max_gap_s,
                        "min_dur": rule.min_duration_s,
                    },
                )
                found += [{"rule": name, "start_s": r.start_s, "end_s": r.end_s} for r in rows]
        return sorted(found, key=lambda e: (e["start_s"], e["rule"]))
