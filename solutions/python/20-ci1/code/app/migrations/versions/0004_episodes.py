"""episodes

Revision ID: 0004
"""

import sqlalchemy as sa
from alembic import op

revision = "0004"
down_revision = "0003"


def upgrade() -> None:
    op.create_table(
        "episodes",
        # What the episode is (video, rule, start), hashed: a rerun computes
        # the same key, finds the row there, and adds nothing.
        sa.Column("key", sa.Text, primary_key=True),
        sa.Column("video_id", sa.Text, sa.ForeignKey("results.videos.id"), nullable=False),
        sa.Column("rule", sa.Text, nullable=False),
        sa.Column("start_s", sa.Float, nullable=False),
        sa.Column("end_s", sa.Float, nullable=False),
        schema="results",
    )
    op.create_index("episodes_video_start", "episodes", ["video_id", "start_s"], schema="results")


def downgrade() -> None:
    op.drop_table("episodes", schema="results")
