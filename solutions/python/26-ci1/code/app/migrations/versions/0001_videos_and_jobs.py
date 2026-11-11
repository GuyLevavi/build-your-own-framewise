"""videos and jobs

Revision ID: 0001
"""

import sqlalchemy as sa
from alembic import op

revision = "0001"
down_revision = None


def upgrade() -> None:
    op.execute("CREATE SCHEMA IF NOT EXISTS results")
    op.execute("CREATE SCHEMA IF NOT EXISTS queue")
    op.create_table(
        "videos",
        sa.Column("id", sa.Text, primary_key=True),
        sa.Column("filename", sa.Text, nullable=False),
        sa.Column("size_bytes", sa.BigInteger, nullable=False),
        sa.Column("path", sa.Text, nullable=False),
        sa.Column(
            "created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
        schema="results",
    )
    op.create_table(
        "jobs",
        sa.Column("id", sa.Text, primary_key=True),
        sa.Column("video_id", sa.Text, sa.ForeignKey("results.videos.id"), nullable=False),
        sa.Column("profile", sa.Text, nullable=False),
        sa.Column("status", sa.Text, nullable=False),
        sa.Column("error", sa.Text),
        sa.Column("labels", sa.JSON),
        sa.Column(
            "created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
        sa.Column(
            "updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False
        ),
        schema="queue",
    )
    op.create_index("jobs_status_created", "jobs", ["status", "created_at"], schema="queue")


def downgrade() -> None:
    op.drop_table("jobs", schema="queue")
    op.drop_table("videos", schema="results")
