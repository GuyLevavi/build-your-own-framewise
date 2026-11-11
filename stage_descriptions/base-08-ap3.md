In this stage, you'll keep videos and jobs in Postgres, so a restart loses nothing.

Everything has lived in memory so far, so restarting the server loses every upload and every accepted job. A service is restarted for every deploy, so its state belongs in a database.

### Expected behaviour

- `./your_program.sh migrate` creates or upgrades the schema in the database at `DATABASE_URL` (a `postgresql://` URL) and exits with 0. Running it twice is fine.
- The schema has two Postgres schemas: `queue` holds the operational, short-lived job records (`queue.jobs`, keyed by an `id` column holding the job id the API returns), and `results` holds what the service produces (`results.videos`, later more). Upload files stay under `--data-dir`.
- `serve` reads `DATABASE_URL`. After a restart on the same database and `--data-dir`:
  - uploads made before it still answer `GET /videos/{id}`;
  - finished jobs are unchanged;
  - jobs that were `PENDING` or `RUNNING` when the server stopped are picked up again and reach `DONE`. A job left `RUNNING` was interrupted, not done.

### Tests

The tester creates a fresh database, runs `migrate`, and checks that `queue.jobs` and `results.videos` exist. It runs a job to `DONE`, submits two jobs on the long clip, and stops the server with SIGTERM right away. It checks that `results.videos` has 2 rows and `queue.jobs` has 3. It then starts a new server on the same database and `--data-dir`, checks the three points above, and runs one more job.

The tester uses the Postgres at `FRAMEWISE_TEST_DATABASE_URL` (it creates and drops `fw_test_*` databases there) or, if that's unset, starts one with podman.

### Why your design just broke

If your handlers reached into a module-level dict, every one of them changes now. The fix is a repository: one object with methods like `add_job`, `job(id)` and `set_status`, which the API and the worker call and which alone knows about storage. Swapping the in-memory version for Postgres should then touch that object and nothing else. This is also where `app/main.py` usually stops being one file.

### Pointers

- [SQLAlchemy Unified Tutorial](https://docs.sqlalchemy.org/en/20/tutorial/index.html) (engine, transactions, Core or ORM) and the [Alembic tutorial](https://alembic.sqlalchemy.org/en/latest/tutorial.html). `alembic.command.upgrade` runs migrations from Python.
- Search: `postgresql+psycopg URL`, `alembic version_table_schema`, `CREATE SCHEMA`.
