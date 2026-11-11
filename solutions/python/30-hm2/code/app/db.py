"""Postgres: the engine, and the tables in their two schemas.

queue holds jobs (operational, short-lived); results holds what the service
produces. Migrations in app/migrations create them; `migrate` runs those.
"""

import os
from pathlib import Path

import sqlalchemy as sa
from alembic import command
from alembic.config import Config

from app.errors import FramewiseError

metadata = sa.MetaData()

videos = sa.Table(
    "videos",
    metadata,
    sa.Column("id", sa.Text, primary_key=True),
    sa.Column("filename", sa.Text, nullable=False),
    sa.Column("size_bytes", sa.BigInteger, nullable=False),
    sa.Column("path", sa.Text, nullable=False),
    sa.Column(
        "created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
    ),
    schema="results",
)

jobs = sa.Table(
    "jobs",
    metadata,
    sa.Column("id", sa.Text, primary_key=True),
    sa.Column("video_id", sa.Text, sa.ForeignKey("results.videos.id"), nullable=False),
    sa.Column("profile", sa.Text, nullable=False),
    sa.Column("status", sa.Text, nullable=False),
    sa.Column("error", sa.Text),
    sa.Column("labels", sa.JSON),
    sa.Column("attempts", sa.Integer, nullable=False, server_default="0"),
    sa.Column("lease_until", sa.DateTime(timezone=True)),
    sa.Column("frames", sa.Integer),
    sa.Column("analysis_s", sa.Float),
    sa.Column("progress", sa.Float, nullable=False, server_default="0"),
    sa.Column(
        "created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
    ),
    sa.Column(
        "updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
    ),
    sa.Index("jobs_status_created", "status", "created_at"),
    schema="queue",
)

detections = sa.Table(
    "detections",
    metadata,
    sa.Column("id", sa.BigInteger, sa.Identity(), primary_key=True),
    sa.Column("job_id", sa.Text, sa.ForeignKey("queue.jobs.id"), nullable=False),
    sa.Column("frame", sa.Integer, nullable=False),
    sa.Column("t_s", sa.Float, nullable=False),
    sa.Column("label", sa.Text, nullable=False),
    sa.Column("x", sa.Integer, nullable=False),
    sa.Column("y", sa.Integer, nullable=False),
    sa.Column("w", sa.Integer, nullable=False),
    sa.Column("h", sa.Integer, nullable=False),
    sa.Column("score", sa.Float, nullable=False),
    sa.Index("detections_job_time", "job_id", "t_s"),
    schema="results",
)

episodes = sa.Table(
    "episodes",
    metadata,
    sa.Column("key", sa.Text, primary_key=True),
    sa.Column("video_id", sa.Text, sa.ForeignKey("results.videos.id"), nullable=False),
    sa.Column("rule", sa.Text, nullable=False),
    sa.Column("start_s", sa.Float, nullable=False),
    sa.Column("end_s", sa.Float, nullable=False),
    sa.Index("episodes_video_start", "video_id", "start_s"),
    schema="results",
)


def database_url() -> str:
    url = os.environ.get("DATABASE_URL")
    if not url:
        raise FramewiseError("DATABASE_URL is not set")
    # SQLAlchemy picks the driver from the scheme; we use psycopg 3.
    for prefix in ("postgresql://", "postgres://"):
        if url.startswith(prefix):
            return "postgresql+psycopg://" + url[len(prefix) :]
    return url


def libpq_url() -> str:
    """DATABASE_URL as psycopg itself takes it, for connections outside SQLAlchemy."""
    return database_url().replace("postgresql+psycopg://", "postgresql://", 1)


def make_engine() -> sa.Engine:
    # Fail fast when Postgres is unreachable instead of hanging a request or a worker:
    # callers turn the error into a 503 or a retry.
    return sa.create_engine(database_url(), pool_pre_ping=True, connect_args={"connect_timeout": 3})


def migrate() -> None:
    config = Config()
    config.set_main_option("script_location", str(Path(__file__).parent / "migrations"))
    config.set_main_option("sqlalchemy.url", database_url().replace("%", "%%"))
    command.upgrade(config, "head")
