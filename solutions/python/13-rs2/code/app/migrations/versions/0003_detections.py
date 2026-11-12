"""detections

Revision ID: 0003
"""

import sqlalchemy as sa
from alembic import op

revision = "0003"
down_revision = "0002"


def upgrade() -> None:
    op.create_table(
        "detections",
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
        schema="results",
    )
    # Detections are read per job and time range: this index serves exactly that.
    op.create_index("detections_job_time", "detections", ["job_id", "t_s"], schema="results")


def downgrade() -> None:
    op.drop_table("detections", schema="results")
