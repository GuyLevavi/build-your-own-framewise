"""job progress

Revision ID: 0004
"""

import sqlalchemy as sa
from alembic import op

revision = "0004"
down_revision = "0003"


def upgrade() -> None:
    op.add_column(
        "jobs", sa.Column("progress", sa.Float, nullable=False, server_default="0"), schema="queue"
    )


def downgrade() -> None:
    op.drop_column("jobs", "progress", schema="queue")
