package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
	"github.com/jackc/pgx/v5"
)

// sample is one line of Prometheus text exposition.
type sample struct {
	name   string
	labels map[string]string
	value  float64
}

func parseMetrics(text string) []sample {
	var result []sample
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cut := strings.LastIndexByte(line, ' ')
		if cut < 0 {
			continue
		}
		value, err := strconv.ParseFloat(line[cut+1:], 64)
		if err != nil {
			continue
		}
		head := line[:cut]
		s := sample{name: head, labels: map[string]string{}, value: value}
		if open := strings.IndexByte(head, '{'); open >= 0 && strings.HasSuffix(head, "}") {
			s.name = head[:open]
			for _, pair := range strings.Split(head[open+1:len(head)-1], ",") {
				k, v, ok := strings.Cut(pair, "=")
				if ok {
					s.labels[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
				}
			}
		}
		result = append(result, s)
	}
	return result
}

func (s *server) metrics() (string, error) {
	r, err := s.get("/metrics", nil)
	if err != nil {
		return "", err
	}
	if r.status != 200 {
		return "", fmt.Errorf("expected GET /metrics to answer 200, got %s", r.describe())
	}
	return string(r.raw), nil
}

func testMetrics(harness *test_case_harness.TestCaseHarness) error {
	_, env, err := workerStage(harness, nil)
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
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
	text, err := sv.metrics()
	if err != nil {
		return err
	}
	samples := parseMetrics(text)
	byStatus := map[string]float64{}
	for _, s := range samples {
		if s.name == "framewise_jobs" {
			byStatus[s.labels["status"]] = s.value
		}
	}
	if byStatus["DONE"] != 2 {
		return fmt.Errorf(`expected framewise_jobs{status="DONE"} 2 in GET /metrics (the jobs in the database, whichever process ran them), got %v`, byStatus)
	}
	harness.Logger.Successf(`✓ framewise_jobs{status="DONE"} 2`)
	analyzed := -1.0
	for _, s := range samples {
		if s.name == "framewise_frames_analyzed_total" {
			analyzed = s.value
		}
	}
	if analyzed != float64(2*frames) {
		return fmt.Errorf(`expected framewise_frames_analyzed_total %d in GET /metrics (frames analyzed by finished jobs, from the database), got %v`, 2*frames, analyzed)
	}
	harness.Logger.Successf("✓ framewise_frames_analyzed_total %d", 2*frames)
	routed := false
	for _, s := range samples {
		if s.name == "framewise_http_request_duration_seconds_count" && strings.Contains(s.labels["route"], "/jobs/{") && s.value > 0 {
			routed = true
		}
	}
	if !routed {
		return fmt.Errorf(`expected a histogram framewise_http_request_duration_seconds with a "route" label holding the route's template, like route="/jobs/{job_id}", and a count above 0 for it`)
	}
	harness.Logger.Successf("✓ framewise_http_request_duration_seconds by route")
	for _, id := range jobs {
		if strings.Contains(text, id) {
			return fmt.Errorf("expected no job id in GET /metrics, found %s. Every distinct label value is a new time series: label by route template, never by raw path", id)
		}
	}
	harness.Logger.Successf("✓ no ids in metric labels")

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
	return nil
}

type progressEvent struct {
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
}

// readProgress reads GET /jobs/{id}/progress as server-sent events until the server ends it.
func (s *server) readProgress(jobID string, timeout time.Duration) ([]progressEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", s.base+"/jobs/"+jobID+"/progress", nil)
	request.Header.Set("Accept", "text/event-stream")
	if s.apiKey != "" {
		request.Header.Set("X-API-Key", s.apiKey)
	}
	s.harness.Logger.Infof("GET /jobs/%s/progress (server-sent events)", jobID)
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("expected status 200, got %d", response.StatusCode)
	}
	if ct := response.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		return nil, fmt.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	var events []progressEvent
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		var e progressEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &e); err != nil {
			return nil, fmt.Errorf(`expected each event's data to be JSON {"status", "progress"}, got %q`, truncate(data))
		}
		events = append(events, e)
	}
	if ctx.Err() != nil {
		return events, fmt.Errorf("expected the stream to end once the job finished, it was still open after %s (%d events)", timeout, len(events))
	}
	return events, nil
}

// listen collects NOTIFY payloads on channel until stop is called.
func listen(dbURL, channel string) (payloads func() []string, stop func(), err error) {
	ctx, cancel := context.WithCancel(context.Background())
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		cancel()
		return nil, nil, err
	}
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			n, err := conn.WaitForNotification(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			got = append(got, n.Payload)
			mu.Unlock()
		}
	}()
	payloads = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
	stop = func() {
		cancel()
		<-done
		conn.Close(context.Background())
	}
	return payloads, stop, nil
}

func testProgress(harness *test_case_harness.TestCaseHarness) error {
	dbURL, env, err := workerStage(harness, nil)
	if err != nil {
		return err
	}
	payloads, stopListening, err := listen(dbURL, "framewise_progress")
	if err != nil {
		return fmt.Errorf("CodeCrafters Internal Error: LISTEN: %v", err)
	}
	defer stopListening()
	harness.Logger.Infof("Listening on channel framewise_progress")
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	longID, err := sv.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(longID, "")
	if err != nil {
		return err
	}
	events, err := sv.readProgress(jobID, 150*time.Second)
	if err != nil {
		return err
	}
	var running []float64
	for i, e := range events {
		if i > 0 && e.Progress < events[i-1].Progress-1e-9 {
			return fmt.Errorf("expected progress never to go down, got %.3f after %.3f", e.Progress, events[i-1].Progress)
		}
		if e.Status == "RUNNING" && (len(running) == 0 || e.Progress > running[len(running)-1]) {
			running = append(running, e.Progress)
		}
	}
	if len(running) < 3 {
		return fmt.Errorf("expected at least 3 RUNNING events with growing progress while the long clip was analyzed, got %d (%d events in all)", len(running), len(events))
	}
	if len(events) == 0 || events[len(events)-1].Status != "DONE" || math.Abs(events[len(events)-1].Progress-1) > 1e-6 {
		return fmt.Errorf(`expected the last event to be {"status": "DONE", "progress": 1}, got %+v`, events[len(events)-1:])
	}
	harness.Logger.Successf("✓ %d events, %d steps of progress, ending DONE", len(events), len(running))

	var notified []float64
	for _, payload := range payloads() {
		var p struct {
			JobID    string  `json:"job_id"`
			Progress float64 `json:"progress"`
		}
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return fmt.Errorf(`expected NOTIFY framewise_progress payloads to be JSON {"job_id", "progress"}, got %q`, truncate(payload))
		}
		if p.JobID == jobID {
			notified = append(notified, p.Progress)
		}
	}
	if len(notified) < 3 {
		return fmt.Errorf("expected at least 3 notifications on channel framewise_progress for the job, got %d", len(notified))
	}
	harness.Logger.Successf("✓ %d notifications on framewise_progress", len(notified))

	start := time.Now()
	events, err = sv.readProgress(jobID, 10*time.Second)
	if err != nil {
		return fmt.Errorf("progress of a finished job: %v", err)
	}
	if len(events) != 1 || events[0].Status != "DONE" {
		return fmt.Errorf("expected a finished job's stream to send one DONE event and end, got %+v", events)
	}
	harness.Logger.Successf("✓ a finished job's stream ends at once (%.1f s)", time.Since(start).Seconds())
	r, err := sv.get("/jobs/"+randomID()+"/progress", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 404); err != nil {
		return fmt.Errorf("progress of an unknown job: %v", err)
	}
	harness.Logger.Successf("✓ 404 for an unknown job")
	return nil
}

func testOutage(harness *test_case_harness.TestCaseHarness) error {
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
	env := serviceEnv(harness, proxied, map[string]string{"FRAMEWISE_LEASE_S": "3"})
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	r, err := sv.get("/ready", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return fmt.Errorf("GET /ready with the database up: %v", err)
	}
	harness.Logger.Successf("✓ /ready 200")
	longID, err := sv.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(longID, "")
	if err != nil {
		return err
	}
	if _, err := sv.waitStatus(jobID, 30*time.Second, "RUNNING"); err != nil {
		return err
	}
	time.Sleep(time.Second)
	harness.Logger.Infof("Cutting the database off for 8 s")
	proxy.down()
	time.Sleep(500 * time.Millisecond)
	checks := []struct {
		path string
		want int
	}{{"/health", 200}, {"/ready", 503}, {"/jobs/" + jobID, 503}}
	for _, c := range checks {
		r, err := sv.get(c.path, nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, c.want); err != nil {
			return fmt.Errorf("GET %s with the database down: %v", c.path, err)
		}
		if r.took > 5*time.Second {
			return fmt.Errorf("GET %s with the database down took %.1f s: fail fast instead", c.path, r.took.Seconds())
		}
		harness.Logger.Successf("✓ %d in %.1f s", c.want, r.took.Seconds())
	}
	time.Sleep(7 * time.Second)
	for name, p := range map[string]*proc{"server": sv.p.(*proc), "worker": sv.workers[0]} {
		if p.exited() {
			return fmt.Errorf("expected the %s to ride out the outage, it exited with %d", name, p.result.ExitCode)
		}
	}
	harness.Logger.Successf("✓ the server and the worker are still running")
	proxy.up()
	harness.Logger.Infof("The database is back")
	deadline := time.Now().Add(30 * time.Second)
	for {
		r, err := sv.get("/ready", nil)
		if err == nil && r.status == 200 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected GET /ready to answer 200 within 30 s of the database coming back")
		}
		time.Sleep(500 * time.Millisecond)
	}
	harness.Logger.Successf("✓ /ready 200 again")
	if _, err := sv.expectJobDone(jobID, sceneLong, defaultProfile); err != nil {
		return fmt.Errorf("the job that was running during the outage: %v", err)
	}
	n, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if want := len(expectedBoxes(sceneLong, 0, sceneLong.frames-1)); n != int64(want) {
		return fmt.Errorf("expected exactly %d rows in results.detections for that job, found %d", want, n)
	}
	harness.Logger.Successf("✓ exactly %d detections", n)
	if sv.workers[0].exited() {
		return fmt.Errorf("expected the same worker to keep running, it exited")
	}
	return sv.runJob(sceneStreet)
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
		for _, path := range []string{"/jobs/" + randomID(), "/videos/" + randomID(), "/metrics"} {
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
	if _, err := sv.metrics(); err != nil {
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
