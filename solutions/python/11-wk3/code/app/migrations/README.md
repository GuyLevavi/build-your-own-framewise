# Migrations

You'll need this folder from the stage "Survive a restart" (ap3) on. Until then, leave it alone.

A *migration* is a small Python file in `versions/` that changes the database schema one step:
create a table, add a column, add an index. Alembic runs the ones a database hasn't had yet, in
order, and records which ran. So a laptop, CI and the cluster all reach the same schema by
running the same command, and nobody types SQL into a shared database.

`env.py` and `script.py.mako` are already set up. What you write:

1. A `migrate` subcommand that runs every migration not yet applied:

   ```python
   from pathlib import Path

   from alembic import command
   from alembic.config import Config


   def migrate(database_url: str) -> None:
       config = Config()
       config.set_main_option("script_location", str(Path(__file__).parent / "migrations"))
       # Alembic's config treats % specially, and passwords may hold one.
       config.set_main_option("sqlalchemy.url", database_url.replace("%", "%%"))
       command.upgrade(config, "head")
   ```

   SQLAlchemy picks the database driver from the URL's scheme, and this project has psycopg 3,
   so turn `postgresql://...` into `postgresql+psycopg://...` first.

2. One file per schema change in `versions/`, such as `versions/0001_videos_and_jobs.py`:

   ```python
   """videos and jobs"""

   import sqlalchemy as sa
   from alembic import op

   revision = "0001"
   down_revision = None  # the next file says "0001" here: that's how Alembic orders them


   def upgrade() -> None:
       op.execute("CREATE SCHEMA IF NOT EXISTS results")
       op.create_table(
           "videos",
           sa.Column("id", sa.Text, primary_key=True),
           # ...
           schema="results",
       )


   def downgrade() -> None:
       op.drop_table("videos", schema="results")
   ```

   Once a migration has run anywhere, don't edit it: add the next one.

Read more: the [Alembic tutorial](https://alembic.sqlalchemy.org/en/latest/tutorial.html), from
"Create a Migration Script" on (we skip `alembic init`, since this folder is already made).
