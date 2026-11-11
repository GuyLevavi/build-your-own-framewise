package internal

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
	"github.com/jackc/pgx/v5"
)

// The database stages need a Postgres server the tester can create databases on:
//   - FRAMEWISE_TEST_DATABASE_URL, for example the team's hosted instance
//     (postgresql://user:pass@host:5432/postgres); or
//   - one the tester starts itself with podman, from FRAMEWISE_POSTGRES_IMAGE.
//
// Each stage gets a database of its own, fw_test_<word>_<n>, dropped at the end.
const defaultPostgresImage = "docker.io/library/postgres:18"

// stageDatabase creates a fresh database and returns its URL.
func stageDatabase(harness *test_case_harness.TestCaseHarness) (string, error) {
	admin := os.Getenv("FRAMEWISE_TEST_DATABASE_URL")
	if admin == "" {
		var err error
		if admin, err = startPostgres(harness); err != nil {
			return "", err
		}
	}
	name := fmt.Sprintf("fw_test_%s_%d", random.RandomWord(), random.RandomInt(1000, 9999))
	if err := adminExec(admin, "CREATE DATABASE "+name); err != nil {
		return "", fmt.Errorf("couldn't create a test database on FRAMEWISE_TEST_DATABASE_URL: %v", err)
	}
	harness.RegisterTeardownFunc(func() {
		_ = adminExec(admin, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := url.Parse(admin)
	if err != nil {
		return "", fmt.Errorf("FRAMEWISE_TEST_DATABASE_URL is not a URL: %v", err)
	}
	u.Path = "/" + name
	harness.Logger.Infof("Using a fresh database, %s", name)
	return u.String(), nil
}

func adminExec(admin, sql string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

// startPostgres runs a throwaway Postgres in podman (or docker) and returns its admin URL.
func startPostgres(harness *test_case_harness.TestCaseHarness) (string, error) {
	engine, err := exec.LookPath("podman")
	if err != nil {
		if engine, err = exec.LookPath("docker"); err != nil {
			return "", fmt.Errorf("this stage needs Postgres: set FRAMEWISE_TEST_DATABASE_URL to a server the tester may create databases on, or install podman so the tester can start one")
		}
	}
	image := os.Getenv("FRAMEWISE_POSTGRES_IMAGE")
	if image == "" {
		image = defaultPostgresImage
	}
	port := freePort()
	name := fmt.Sprintf("framewise-test-pg-%d", port)
	harness.Logger.Infof("Starting Postgres (%s) on port %d", image, port)
	out, err := exec.Command(engine, "run", "-d", "--rm", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
		"-e", "POSTGRES_HOST_AUTH_METHOD=trust", image).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("couldn't start Postgres with %s: %v: %s", engine, err, strings.TrimSpace(string(out)))
	}
	harness.RegisterTeardownFunc(func() { _ = exec.Command(engine, "rm", "-f", name).Run() })
	admin := fmt.Sprintf("postgresql://postgres@127.0.0.1:%d/postgres", port)
	deadline := time.Now().Add(60 * time.Second)
	for {
		if err := adminExec(admin, "SELECT 1"); err == nil {
			return admin, nil
		} else if time.Now().After(deadline) {
			return "", fmt.Errorf("Postgres in %s didn't accept connections within 60 s: %v", engine, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// queryRows runs sql on the stage's database and returns every row.
func queryRows(dbURL, sql string, args ...any) ([][]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		result = append(result, values)
	}
	return result, rows.Err()
}

// expectTables checks that each "schema.table" exists.
func expectTables(harness *test_case_harness.TestCaseHarness, dbURL string, names ...string) error {
	for _, name := range names {
		schema, table, _ := strings.Cut(name, ".")
		rows, err := queryRows(dbURL, "SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2", schema, table)
		if err != nil {
			return fmt.Errorf("CodeCrafters Internal Error: %v", err)
		}
		if len(rows) == 0 {
			return fmt.Errorf("expected `migrate` to create the table %s (schema %s), but it doesn't exist", name, schema)
		}
	}
	harness.Logger.Successf("✓ tables %s exist", strings.Join(names, ", "))
	return nil
}

// databaseAvailable says whether stageDatabase can work here.
func databaseAvailable() bool {
	if os.Getenv("FRAMEWISE_TEST_DATABASE_URL") != "" {
		return true
	}
	for _, engine := range []string{"podman", "docker"} {
		if _, err := exec.LookPath(engine); err == nil {
			return true
		}
	}
	return false
}

// prepareDatabase gives the stage a fresh, migrated database. Stages before
// ap3 call it too, so that a program that needs DATABASE_URL (any program
// past ap3) still passes them: there it is lenient, and a database that
// can't be made or a `migrate` that fails only means none is used.
func prepareDatabase(harness *test_case_harness.TestCaseHarness, strict bool) (string, error) {
	if !strict && !databaseAvailable() {
		return "", nil
	}
	dbURL, err := stageDatabase(harness)
	if err != nil {
		return "", err
	}
	if !strict {
		result, err := runCLIWithEnv(harness, map[string]string{"DATABASE_URL": dbURL}, "migrate")
		if err != nil || result.ExitCode != 0 {
			harness.Logger.Infof("(no `migrate` yet: fine before stage ap3)")
		}
		return dbURL, nil
	}
	return dbURL, migrate(harness, dbURL)
}

// migrate runs `./your_program.sh migrate` on dbURL.
func migrate(harness *test_case_harness.TestCaseHarness, dbURL string) error {
	var result, err = runCLIWithEnv(harness, map[string]string{"DATABASE_URL": dbURL}, "migrate")
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("expected `migrate` to exit with 0, got %d", result.ExitCode)
	}
	harness.Logger.Successf("✓ migrate exited with 0")
	return nil
}

// lockJobRow plays another worker in the middle of claiming a job: it locks
// the job's row in queue.jobs and holds the lock until release is called.
func lockJobRow(dbURL, jobID string) (release func(), err error) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return nil, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Close(ctx)
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT id FROM queue.jobs WHERE id = $1 FOR UPDATE", jobID)
	if err != nil {
		tx.Rollback(ctx)
		conn.Close(ctx)
		return nil, fmt.Errorf("couldn't lock job %s in queue.jobs (is its id in a column named id?): %v", jobID, err)
	}
	found := rows.Next()
	rows.Close()
	if !found {
		tx.Rollback(ctx)
		conn.Close(ctx)
		return nil, fmt.Errorf("expected a row in queue.jobs with id %q (the id POST /jobs returned), found none", jobID)
	}
	return func() {
		tx.Rollback(ctx)
		conn.Close(ctx)
	}, nil
}
