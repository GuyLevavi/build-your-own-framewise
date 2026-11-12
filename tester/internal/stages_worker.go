package internal

import (
	"fmt"
	"syscall"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// workerStage prepares a database and the environment for a stage from wk1 on.
func workerStage(harness *test_case_harness.TestCaseHarness, extra map[string]string) (string, map[string]string, error) {
	dbURL, err := prepareDatabase(harness, true)
	if err != nil {
		return "", nil, err
	}
	return dbURL, serviceEnv(harness, dbURL, extra), nil
}

// testWorkers is wk1: the API only records jobs, `work` processes run them, and
// several workers share the queue without taking the same job.
func testWorkers(harness *test_case_harness.TestCaseHarness) error {
	if err := testSeparateWorker(harness); err != nil {
		return err
	}
	harness.Logger.Infof("Now several workers on one queue")
	return testManyWorkers(harness)
}

func testSeparateWorker(harness *test_case_harness.TestCaseHarness) error {
	_, env, err := workerStage(harness, nil)
	if err != nil {
		return err
	}
	dataDir := tempDir(harness)
	sv, err := startService(harness, dataDir, env, 0, false)
	if err != nil {
		return err
	}
	streetID, err := sv.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(streetID, "")
	if err != nil {
		return err
	}
	harness.Logger.Infof("No worker is running: the job must stay PENDING for 3 s")
	time.Sleep(3 * time.Second)
	job, err := sv.waitStatus(jobID, time.Second, "PENDING", "RUNNING", "DONE", "FAILED")
	if err != nil {
		return err
	}
	if job["status"] != "PENDING" {
		return fmt.Errorf("expected the job to stay PENDING while no worker runs, got %v. From this stage `serve` only records jobs; `work` runs them", job["status"])
	}
	harness.Logger.Successf("✓ still PENDING")
	if _, err := sv.addWorker(); err != nil {
		return err
	}
	if _, err := sv.expectJobDone(jobID, sceneStreet, defaultProfile); err != nil {
		return err
	}

	longID, err := sv.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	longJob, _, err := sv.submitJob(longID, "sparse")
	if err != nil {
		return err
	}
	if _, err := sv.waitStatus(longJob, 30*time.Second, "RUNNING"); err != nil {
		return err
	}
	harness.Logger.Successf("✓ RUNNING")
	sv.server.p.kill()
	time.Sleep(time.Second)
	if sv.workers[0].exited() {
		return fmt.Errorf("expected the worker to keep running when the server dies, it exited with %d", sv.workers[0].result.ExitCode)
	}
	harness.Logger.Successf("✓ the worker is still running without the server")
	s, err := startServer(harness, dataDir, env)
	if err != nil {
		return err
	}
	_, err = s.expectJobDone(longJob, sceneLong, sparseProfile)
	return err
}

func testManyWorkers(harness *test_case_harness.TestCaseHarness) error {
	tr := newTrace(harness)
	dbURL, env, err := workerStage(harness, tr.env())
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 0, false)
	if err != nil {
		return err
	}
	streetID, err := sv.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	const n = 12
	var jobs []string
	for i := 0; i < n; i++ {
		id, _, err := sv.submitJob(streetID, "")
		if err != nil {
			return err
		}
		jobs = append(jobs, id)
	}
	harness.Logger.Infof("Locking job %s's row, as a worker in the middle of claiming it would", jobs[0])
	release, err := lockJobRow(dbURL, jobs[0])
	if err != nil {
		return err
	}
	defer release()
	for i := 0; i < 3; i++ {
		if _, err := sv.addWorker(); err != nil {
			return err
		}
	}
	for _, id := range jobs[1:] {
		if _, err := sv.waitStatus(id, 60*time.Second, "DONE", "FAILED"); err != nil {
			return fmt.Errorf("%v. Your workers seem to wait for the locked job: claim with FOR UPDATE SKIP LOCKED so a locked row is skipped, not waited for", err)
		}
	}
	harness.Logger.Successf("✓ the other %d jobs finished while one was locked", n-1)
	release()
	harness.Logger.Infof("Released the lock")
	for _, id := range jobs {
		if _, err := sv.expectJobDone(id, sceneStreet, defaultProfile); err != nil {
			return err
		}
	}
	calls, err := tr.lines()
	if err != nil {
		return err
	}
	if want := n * sceneStreet.frames; len(calls) != want {
		return fmt.Errorf("expected each of the %d jobs to be analyzed exactly once (%d detector calls), got %d calls. Two workers took the same job", n, want, len(calls))
	}
	harness.Logger.Successf("✓ %d detector calls: every job analyzed exactly once", len(calls))
	byGroup := map[int]bool{}
	for _, w := range sv.workers {
		for _, c := range calls {
			if c.PID == w.pid || pgidOf(c.PID) == w.pid {
				byGroup[w.pid] = true
			}
		}
	}
	if len(byGroup) < 2 {
		return fmt.Errorf("expected the jobs to be spread over your 3 workers, but only %d of them ran any", len(byGroup))
	}
	harness.Logger.Successf("✓ %d of 3 workers took jobs", len(byGroup))
	return nil
}

// workerEnv for the crash stages: short leases so a dead worker's job comes back quickly.
var crashEnv = map[string]string{"FRAMEWISE_LEASE_S": "3", "FRAMEWISE_MAX_ATTEMPTS": "2"}

// runningWorker waits until the detector is running for the job and returns the worker doing it.
func runningWorker(sv *service, tr trace) (*proc, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		calls, _ := tr.lines()
		if len(calls) > 20 {
			if w := sv.workerOf(calls[len(calls)-1].PID); w != nil {
				return w, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("expected a worker to start analyzing the job within 30 s")
}

func intOf(job map[string]any, key string) int64 {
	n, _ := intField(job, key)
	return n
}

func testCrashedWorkers(harness *test_case_harness.TestCaseHarness) error {
	tr := newTrace(harness)
	extra := tr.env()
	for k, v := range crashEnv {
		extra[k] = v
	}
	_, env, err := workerStage(harness, extra)
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 2, false)
	if err != nil {
		return err
	}
	sv.supervise()
	longID, err := sv.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(longID, "traffic")
	if err != nil {
		return err
	}
	w, err := runningWorker(sv, tr)
	if err != nil {
		return err
	}
	w.kill()
	job, err := sv.expectJobDone(jobID, sceneLong, testProfiles["traffic"])
	if err != nil {
		return fmt.Errorf("a job whose worker was killed: %v. Give claims a lease the worker renews, and reclaim jobs whose lease ran out (FRAMEWISE_LEASE_S=3 here)", err)
	}
	if a := intOf(job, "attempts"); a != 2 {
		return fmt.Errorf(`expected the job to report "attempts": 2 (one killed, one finished), got %v`, job["attempts"])
	}
	harness.Logger.Successf("✓ reclaimed and finished, attempts 2")

	poisonID, err := sv.uploadClip("poison.avi", scenePoison.bytes())
	if err != nil {
		return err
	}
	jobID, _, err = sv.submitJob(poisonID, "")
	if err != nil {
		return err
	}
	job, err = sv.waitStatus(jobID, 90*time.Second, "FAILED", "DONE")
	if err != nil {
		return fmt.Errorf("a job that kills every worker that runs it: %v. After FRAMEWISE_MAX_ATTEMPTS (2) attempts it must be FAILED", err)
	}
	if job["status"] != "FAILED" || intOf(job, "attempts") != 2 {
		return fmt.Errorf(`expected the poison job to end FAILED with "attempts": 2, got %v with attempts %v`, job["status"], job["attempts"])
	}
	if message, _ := job["error"].(string); message == "" {
		return fmt.Errorf("expected the FAILED job to have a non-empty \"error\"")
	}
	harness.Logger.Successf("✓ FAILED after 2 attempts: %v", job["error"])
	return sv.runJob(sceneStreet)
}

func testGracefulShutdown(harness *test_case_harness.TestCaseHarness) error {
	tr := newTrace(harness)
	_, env, err := workerStage(harness, map[string]string{"FRAMEWISE_TRACE": tr.path, "FRAMEWISE_LEASE_S": "60"})
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	longID, err := sv.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(longID, "sparse")
	if err != nil {
		return err
	}
	w, err := runningWorker(sv, tr)
	if err != nil {
		return err
	}
	harness.Logger.Infof("Sending SIGTERM to the worker mid-job")
	start := time.Now()
	w.signal(syscall.SIGTERM)
	if !w.wait(5 * time.Second) {
		w.kill()
		return fmt.Errorf("expected the worker to exit within 5 s of SIGTERM, it was still running")
	}
	harness.Logger.Successf("✓ exited in %.1f s", time.Since(start).Seconds())
	if code := w.result.ExitCode; code != 0 {
		return fmt.Errorf("expected the worker to exit with 0 after SIGTERM, got %d", code)
	}
	harness.Logger.Successf("✓ exit code 0")
	job, err := sv.waitStatus(jobID, time.Second, "PENDING", "RUNNING", "DONE", "FAILED")
	if err != nil {
		return err
	}
	if job["status"] != "PENDING" && job["status"] != "DONE" {
		return fmt.Errorf("expected the stopped worker to finish the job or give it back (PENDING), got %v. Its lease is 60 s here, so nobody else would pick it up for a minute", job["status"])
	}
	harness.Logger.Successf("✓ the job is %v", job["status"])
	if _, err := sv.addWorker(); err != nil {
		return err
	}
	_, err = sv.expectJobDone(jobID, sceneLong, sparseProfile)
	return err
}
