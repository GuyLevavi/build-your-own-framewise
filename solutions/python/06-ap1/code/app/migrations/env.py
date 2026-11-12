"""Alembic's entry point: it runs the files in versions/ against the database.

You don't need to change this file. Your `migrate` command builds a Config that points here and
holds the database URL (see README.md next to this file), then calls command.upgrade.
"""

import sqlalchemy as sa
from alembic import context

config = context.config
engine = sa.create_engine(config.get_main_option("sqlalchemy.url"))
with engine.connect() as connection:
    # Alembic remembers which migrations ran in a table of its own, alembic_version. Keep it in
    # public, away from the queue and results schemas your migrations create.
    context.configure(connection=connection, version_table_schema="public")
    with context.begin_transaction():
        context.run_migrations()
