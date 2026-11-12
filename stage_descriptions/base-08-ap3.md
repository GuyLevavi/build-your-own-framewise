In this stage, you'll keep videos and jobs in Postgres, so a restart loses nothing.

Everything has lived in memory so far, so restarting the server loses every upload and every accepted job. A service is restarted for every deploy, so its state belongs in a database.

### New here?

- **Postgres**: the database the team uses. Data lives in tables (rows and columns), grouped in *schemas* such as `queue` and `results`.
- **SQLAlchemy**: the Python library you'll use to talk to Postgres. An *engine* holds the connections; `with engine.begin() as conn:` runs statements in one transaction.
- **Migration**: a versioned script that changes the schema one step. **Alembic** runs the ones a database hasn't had yet. The setup is already in `app/migrations/`.
- **Repository**: one class that owns all reading and writing of your records, so the rest of the code never sees SQL.

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

- `app/migrations/README.md`, in your own code: Alembic is already set up there. It shows the `migrate` function and the shape of a first migration file, so what's left is your schema and the repository.
- [SQLAlchemy Unified Tutorial](https://docs.sqlalchemy.org/en/20/tutorial/index.html) (engine, transactions, Core or ORM) and the [Alembic tutorial](https://alembic.sqlalchemy.org/en/latest/tutorial.html).
- Search: `postgresql+psycopg URL`, `alembic version_table_schema`, `CREATE SCHEMA`.

### In review

The tester can't check this part, so a reviewer will: you'll be asked to show the one object that owns all SQL, migrations rather than `create_all`, and what each of the two schemas is for.

### Hints

Try the stage first. Open a hint when you're stuck, one at a time.

<details>
<summary>Hint 1: the idea</summary>

First, move every read and write of jobs and videos behind one class (`add_video`, `video(id)`, `add_job`, `job(id)`, `set_status`, `pending_jobs()`), still backed by dicts, and get the earlier stages passing again. Then write a Postgres version of that class. Nothing else should change.

</details>

<details>
<summary>Hint 2: the shape of a solution</summary>

Follow `app/migrations/README.md`: a `migrate` subcommand, and `versions/0001_videos_and_jobs.py` that runs `CREATE SCHEMA IF NOT EXISTS results` and `queue` and creates `results.videos` and `queue.jobs`. Describe the same tables with `sa.Table(..., schema="queue")` in a `db.py` so the repository can use `sa.insert(jobs).values(...)` and `sa.select(jobs).where(jobs.c.id == job_id)`. On startup, `serve` puts every `PENDING` or `RUNNING` job back on the in-memory queue.

</details>

<details>
<summary>Hint 3: the usual traps</summary>

`DATABASE_URL` is `postgresql://...`; SQLAlchemy needs `postgresql+psycopg://...` to use psycopg 3. A job left `RUNNING` by a crash was interrupted: requeue it, don't treat it as done. Store `labels` in a `JSON` column, not as a string you parse later.

</details>
