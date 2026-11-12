package internal

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

func testLogsAndReadiness(harness *test_case_harness.TestCaseHarness) error {
	dbURL, err := prepareDatabase(harness, true)
	if err != nil {
		return err
	}
	proxy, proxied, err := startDBProxy(dbURL)
	if err != nil {
		return fmt.Errorf("CodeCrafters Internal Error: %v", err)
	}
	harness.RegisterTeardownFunc(proxy.close)
	harness.Logger.Infof("Your program reaches Postgres through the tester, which can cut it off")
	env := serviceEnv(harness, proxied, nil)
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	if err := expectReady(sv, 200, "with the database up"); err != nil {
		return err
	}
	videoID, err := sv.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	var jobs []string
	frames := sampledFrames(sceneStreet, defaultProfile)
	for i := 0; i < 2; i++ {
		submitted := time.Now()
		id, _, err := sv.submitJob(videoID, "")
		if err != nil {
			return err
		}
		job, err := sv.expectJobDone(id, sceneStreet, defaultProfile)
		if err != nil {
			return err
		}
		elapsed := time.Since(submitted).Seconds()
		jobs = append(jobs, id)
		if n, err := intField(job, "frames"); err != nil || n != int64(frames) {
			return fmt.Errorf(`expected GET /jobs/%s to have "frames": %d (the frames the detector analyzed), got %v`, id, frames, job["frames"])
		}
		fps, err := floatField(job, "fps")
		if err != nil {
			return fmt.Errorf("GET /jobs/%s: %v (frames analyzed per second of the job's run)", id, err)
		}
		if fps <= 0 || float64(frames)/fps > elapsed+1 {
			return fmt.Errorf(`expected "fps" to be the frames analyzed per second while the job ran: %d frames took at most %.1f s, so at least %.0f, got %v`, frames, elapsed, float64(frames)/(elapsed+1), fps)
		}
		harness.Logger.Successf("✓ %d frames at %.0f frames/s", frames, fps)
	}
	w := sv.workers[0]
	w.stop()
	if !w.exited() {
		return fmt.Errorf("expected the worker to exit on SIGTERM")
	}
	var tagged, withFPS int
	for _, line := range strings.Split(w.stderr(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, jobs[0]) {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return fmt.Errorf("expected every log line about a job to be one JSON object, got %q", truncate(line))
		}
		for _, key := range []string{"level", "message"} {
			if _, ok := entry[key].(string); !ok {
				return fmt.Errorf("expected log lines to have string fields \"level\" and \"message\", got %q", truncate(line))
			}
		}
		if entry["job_id"] == jobs[0] {
			tagged++
			if _, ok := entry["fps"].(float64); ok {
				withFPS++
			}
		}
	}
	if tagged < 2 {
		return fmt.Errorf(`expected the worker to log at least 2 JSON lines with "job_id": %q on stderr (taking the job, finishing it), found %d`, jobs[0], tagged)
	}
	harness.Logger.Successf("✓ %d JSON log lines with the job's id", tagged)
	if withFPS == 0 {
		return fmt.Errorf(`expected the line that says the job finished to carry its speed as a number field, "fps", so Splunk can chart it`)
	}
	harness.Logger.Successf(`✓ a log line with the job's "fps"`)

	harness.Logger.Infof("Cutting the database off")
	proxy.down()
	time.Sleep(500 * time.Millisecond)
	r, err := sv.get("/health", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return fmt.Errorf("GET /health with the database down: %v. Liveness says the process works; if it checked the database, an outage would make Kubernetes restart every replica", err)
	}
	harness.Logger.Successf("✓ /health 200")
	if err := expectReady(sv, 503, "with the database down"); err != nil {
		return err
	}
	proxy.up()
	harness.Logger.Infof("The database is back")
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, err := sv.get("/ready", nil)
		if err == nil && r.status == 200 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected GET /ready to answer 200 within 30 s of the database coming back, without a restart")
		}
		time.Sleep(500 * time.Millisecond)
	}
	harness.Logger.Successf("✓ /ready 200 again")
	return nil
}

// expectReady checks GET /ready answers want, within 5 s.
func expectReady(sv *service, want int, when string) error {
	r, err := sv.get("/ready", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, want); err != nil {
		return fmt.Errorf("GET /ready %s: %v", when, err)
	}
	if r.took > 5*time.Second {
		return fmt.Errorf("GET /ready %s took %.1f s: fail fast instead (a connect timeout)", when, r.took.Seconds())
	}
	sv.harness.Logger.Successf("✓ /ready %d in %.1f s", want, r.took.Seconds())
	return nil
}

func testAPIKey(harness *test_case_harness.TestCaseHarness) error {
	key := "fw-" + randomID() + "-" + randomID()
	_, env, err := workerStage(harness, map[string]string{"FRAMEWISE_API_KEY": key})
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	r, err := sv.get("/health", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return fmt.Errorf("GET /health needs no key: %v", err)
	}
	wrong := key[:len(key)-1] + "x"
	for _, headers := range []map[string]string{{"X-API-Key": ""}, {"X-API-Key": wrong}} {
		what := "without a key"
		if headers["X-API-Key"] != "" {
			what = "with a wrong key"
		}
		for _, path := range []string{"/jobs/" + randomID(), "/videos/" + randomID(), "/jobs/" + randomID() + "/detections"} {
			r, err := sv.get(path, headers)
			if err != nil {
				return err
			}
			if err := expectStatus(r, 401); err != nil {
				return fmt.Errorf("GET %s %s: %v", path, what, err)
			}
		}
		r, err := sv.upload("street.avi", sceneStreet.bytes(), headers)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 401); err != nil {
			return fmt.Errorf("POST /videos %s: %v", what, err)
		}
		harness.Logger.Successf("✓ 401 %s", what)
	}
	sv.apiKey = key
	harness.Logger.Infof("Sending X-API-Key from now on")
	if err := sv.runJob(sceneStreet); err != nil {
		return err
	}
	sv.stop()
	for _, p := range append([]*proc{sv.p.(*proc)}, sv.workers...) {
		p.stop()
		if strings.Contains(p.stderr(), key) {
			return fmt.Errorf("expected the API key never to be logged, but the %s's stderr contains it", p.name)
		}
	}
	harness.Logger.Successf("✓ the key isn't in the logs")
	return nil
}
