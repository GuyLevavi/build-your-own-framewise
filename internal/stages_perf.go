package internal

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// callRate is detector calls per second over the calls in lines, from the first
// call's time to the last's, so process start-up isn't counted.
func callRate(lines []traceLine) float64 {
	if len(lines) < 2 {
		return 0
	}
	first, last := lines[0].T, lines[0].T
	for _, l := range lines {
		first, last = min(first, l.T), max(last, l.T)
	}
	if last <= first {
		return 0
	}
	return float64(len(lines)-1) / (last - first)
}

// timedJob runs a job on sc with a fresh trace and returns the detector's call rate during it.
func timedJob(sv *service, tr trace, sc scene, videoID string) (string, float64, []traceLine, error) {
	tr.reset()
	jobID, _, err := sv.submitJob(videoID, "")
	if err != nil {
		return "", 0, nil, err
	}
	if _, err := sv.expectJobDone(jobID, sc, defaultProfile); err != nil {
		return "", 0, nil, err
	}
	calls, err := tr.lines()
	if err != nil {
		return "", 0, nil, err
	}
	return jobID, callRate(calls), calls, nil
}

// expectDetectionsWithin compares a time range of a job's stored detections with sc, positions within tolerance px.
func expectDetectionsWithin(sv *service, jobID string, sc scene, fromS, toS float64, tolerance int) error {
	r, err := sv.get(fmt.Sprintf("/jobs/%s/detections?from_s=%g&to_s=%g", jobID, fromS, toS), nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return err
	}
	got, err := detectionsField(r.object)
	if err != nil {
		return err
	}
	from, to := int(fromS*float64(sc.fps)+0.5), int(toS*float64(sc.fps)+0.5)
	if err := compareDetectionsWithin(got, sc, from, to, tolerance); err != nil {
		return fmt.Errorf("%v. Boxes are stored in the coordinates of the original video, whatever size you run the detector at", err)
	}
	sv.harness.Logger.Successf("✓ %d detections between %g and %g s, in %dx%d coordinates", len(got), fromS, toS, sc.width, sc.height)
	return nil
}

func testDownscale(harness *test_case_harness.TestCaseHarness) error {
	tr := newTrace(harness)
	_, env, err := workerStage(harness, map[string]string{"FWDETECT_TRACE": tr.path})
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	streetID, err := sv.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	hdID, err := sv.uploadClip("hd_street.avi", sceneHD.bytes())
	if err != nil {
		return err
	}
	harness.Logger.Infof("Calibrating: a job on the 640x360 street clip")
	_, base, _, err := timedJob(sv, tr, sceneStreet, streetID)
	if err != nil {
		return err
	}
	harness.Logger.Successf("✓ %.0f frames/s at 640x360", base)
	harness.Logger.Infof("A job on the 1920x1080 clip")
	jobID, rate, calls, err := timedJob(sv, tr, sceneHD, hdID)
	if err != nil {
		return err
	}
	widest := 0
	for _, c := range calls {
		widest = max(widest, c.W)
	}
	const want = 0.35
	if rate < want*base {
		return fmt.Errorf("expected at least %.0f frames/s on the 1080p clip (%.2f of the 640x360 rate on this machine), got %.0f; the detector saw frames up to %d px wide. Its cost grows with pixels and its accuracy doesn't", want*base, want, rate, widest)
	}
	harness.Logger.Successf("✓ %.0f frames/s at 1920x1080 (%.2f of the 640x360 rate), frames up to %d px wide", rate, rate/base, widest)
	return expectDetectionsWithin(sv, jobID, sceneHD, 8, 10, 6)
}

// speedupTarget is what n processes must reach against one: 2.2x for 4 cores.
func speedupTarget(n int) float64 { return 1 + 0.4*float64(n-1) }

// detectProcesses is how many processes pf2 asks for: the CPUs this program
// may use (a container's CPU quota counts), at most 4.
func detectProcesses() int { return min(availableCPUs(), 4) }

func availableCPUs() int {
	n := runtime.NumCPU()
	data, err := os.ReadFile("/sys/fs/cgroup/cpu.max")
	if err != nil {
		return n
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[0] == "max" {
		return n
	}
	quota, err1 := strconv.ParseFloat(fields[0], 64)
	period, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil || period <= 0 {
		return n
	}
	return max(1, min(n, int(quota/period)))
}

func testProcesses(harness *test_case_harness.TestCaseHarness) error {
	n := detectProcesses()
	tr := newTrace(harness)
	dbURL, env, err := workerStage(harness, map[string]string{"FWDETECT_TRACE": tr.path, "FRAMEWISE_DETECT_PROCESSES": "1"})
	if err != nil {
		return err
	}
	dataDir := tempDir(harness)
	sv, err := startService(harness, dataDir, env, 1, false)
	if err != nil {
		return err
	}
	hdID, err := sv.uploadClip("hd_street.avi", sceneHD.bytes())
	if err != nil {
		return err
	}
	harness.Logger.Infof("FRAMEWISE_DETECT_PROCESSES=1")
	_, single, _, err := timedJob(sv, tr, sceneHD, hdID)
	if err != nil {
		return err
	}
	harness.Logger.Successf("✓ %.0f frames/s with one process", single)
	sv.workers[0].stop()
	sv.workers = nil

	sv.env = withValue(env, "FRAMEWISE_DETECT_PROCESSES", fmt.Sprint(n))
	harness.Logger.Infof("FRAMEWISE_DETECT_PROCESSES=%d", n)
	w, err := sv.addWorker()
	if err != nil {
		return err
	}
	rss := sampleRSS(w.pid)
	jobID, rate, calls, err := timedJob(sv, tr, sceneHD, hdID)
	peak, _ := rss.finish()
	if err != nil {
		return err
	}
	pids := map[int]bool{}
	for _, c := range calls {
		pids[c.PID] = true
	}
	harness.Logger.Infof("%.0f frames/s with %d processes, %d of them calling the detector; the worker peaked at %.0f MiB", rate, n, len(pids), mib(peak))
	if n < 2 {
		harness.Logger.Infof("(this machine has one CPU: speed-up not checked)")
	} else {
		if len(pids) < n {
			return fmt.Errorf("expected the detector to run in %d processes at once, it ran in %d", n, len(pids))
		}
		if target := speedupTarget(n); rate < target*single {
			return fmt.Errorf("expected at least %.1fx the one-process rate with %d processes (%.0f frames/s), got %.0f (%.1fx). Profile the parent process: whatever it still does for every frame is your ceiling", target, n, target*single, rate, rate/single)
		}
		harness.Logger.Successf("✓ %.1fx faster with %d processes", rate/single, n)
	}
	limit := int64(300+120*n) << 20
	if peak > limit {
		return fmt.Errorf("expected the worker and its processes to stay under %.0f MiB together, they peaked at %.0f MiB. Frames waiting for a process take memory; so do frames nobody reads yet", mib(limit), mib(peak))
	}
	harness.Logger.Successf("✓ peak %.0f MiB (limit %.0f)", mib(peak), mib(limit))
	count, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if want := len(expectedBoxes(sceneHD, 0, sceneHD.frames-1)); count != int64(want) {
		return fmt.Errorf("expected %d rows in results.detections for the job, found %d", want, count)
	}
	return expectDetectionsWithin(sv, jobID, sceneHD, 8, 10, 6)
}

func withValue(env map[string]string, key, value string) map[string]string {
	result := map[string]string{key: value}
	for k, v := range env {
		if k != key {
			result[k] = v
		}
	}
	return result
}

func testPoolCrash(harness *test_case_harness.TestCaseHarness) error {
	n := max(detectProcesses(), 2)
	extra := map[string]string{"FRAMEWISE_DETECT_PROCESSES": fmt.Sprint(n)}
	for k, v := range crashEnv {
		extra[k] = v
	}
	_, env, err := workerStage(harness, extra)
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	poisonID, err := sv.uploadClip("poison.avi", scenePoison.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(poisonID, "")
	if err != nil {
		return err
	}
	harness.Logger.Infof("poison.avi kills the process that runs the detector on frame 30")
	job, err := sv.waitStatus(jobID, 60*time.Second, "FAILED", "DONE")
	if err != nil {
		return fmt.Errorf("%v. A pool process died under your worker; is it waiting for an answer that will never come?", err)
	}
	if job["status"] != "FAILED" {
		return fmt.Errorf("expected the poison job to end FAILED, got %v", job["status"])
	}
	if message, _ := job["error"].(string); message == "" {
		return fmt.Errorf("expected the FAILED job to have a non-empty \"error\"")
	}
	harness.Logger.Successf("✓ FAILED: %v", job["error"])
	if w := sv.workers[0]; w.exited() {
		return fmt.Errorf("expected the worker to survive a crash in one of its processes, it exited with %d", w.result.ExitCode)
	}
	harness.Logger.Successf("✓ the worker is still running")
	return sv.runJob(sceneStreet)
}
