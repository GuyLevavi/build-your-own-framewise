"""job leases and attempts

Revision ID: 0002
"""

import sqlalchemy as sa
from alembic import op

revision = "0002"
down_revision = "0001"


def upgrade() -> None:
    op.add_column(
        "jobs",
        sa.Column("attempts", sa.Integer, nullable=False, server_default="0"),
        schema="queue",
    )
    op.add_column("jobs", sa.Column("lease_until", sa.DateTime(timezone=True)), schema="queue")


def downgrade() -> None:
    op.drop_column("jobs", "lease_until", schema="queue")
    op.drop_column("jobs", "attempts", schema="queue")
