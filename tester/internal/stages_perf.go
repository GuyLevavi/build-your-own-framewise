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
func expectDetectionsWithin(sv *service, jobID string, sc scene, fromS, toS float64, minIoU float64) error {
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
	if err := compareDetectionsWithin(got, sc, from, to, minIoU); err != nil {
		return fmt.Errorf("%v. Boxes are stored in the coordinates of the original video, whatever size you run the detector at", err)
	}
	sv.harness.Logger.Successf("✓ %d detections between %g and %g s, in %dx%d coordinates", len(got), fromS, toS, sc.width, sc.height)
	return nil
}

// From pf1 the profiles file has a profile that runs the model at 320.
const perfProfilesYAML = testProfilesYAML + `  fast:
    every: 1
    max_gap_s: 0.5
    labels: [person, car, bicycle]
    input_size: 320
`

// timedProfileJob is timedJob with a profile.
func timedProfileJob(sv *service, tr trace, sc scene, videoID, profileName string, p profile) (string, float64, []traceLine, error) {
	tr.reset()
	jobID, _, err := sv.submitJob(videoID, profileName)
	if err != nil {
		return "", 0, nil, err
	}
	if _, err := sv.expectJobDone(jobID, sc, p); err != nil {
		return "", 0, nil, err
	}
	calls, err := tr.lines()
	if err != nil {
		return "", 0, nil, err
	}
	return jobID, callRate(calls), calls, nil
}

func largestInput(calls []traceLine) int {
	largest := 0
	for _, c := range calls {
		largest = max(largest, c.W, c.H)
	}
	return largest
}

// pf1's target: the fast profile against the default, both on the 1080p clip.
const fastSpeedup = 1.5

func testDownscale(harness *test_case_harness.TestCaseHarness) error {
	tr := newTrace(harness)
	extra := tr.env()
	extra["FRAMEWISE_PROFILES"] = writeProfiles(harness, perfProfilesYAML)
	_, env, err := workerStage(harness, extra)
	if err != nil {
		return err
	}
	harness.Logger.Infof("Profiles:\n%s", perfProfilesYAML)
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	hdID, err := sv.uploadClip("hd_street.avi", sceneHD.bytes())
	if err != nil {
		return err
	}
	harness.Logger.Infof("A job on the 1920x1080 clip with the default profile")
	_, base, calls, err := timedJob(sv, tr, sceneHD, hdID)
	if err != nil {
		return err
	}
	harness.Logger.Successf("✓ %.1f frames/s, the model's input up to %d px", base, largestInput(calls))
	fast := profile{every: 1, maxGapS: 0.5, labels: allLabels}
	harness.Logger.Infof("A job on the same clip with the fast profile (input_size: 320)")
	jobID, rate, calls, err := timedProfileJob(sv, tr, sceneHD, hdID, "fast", fast)
	if err != nil {
		return err
	}
	if largest := largestInput(calls); largest > 320 {
		return fmt.Errorf("expected the fast profile to run the model on inputs no larger than 320 px a side, it ran on %d px. The model's input size is a setting of the profile", largest)
	}
	if rate < fastSpeedup*base {
		return fmt.Errorf("expected the fast profile to make at least %.1fx the default's frames/s (%.1f), got %.1f (%.2fx). Profile the worker: the model is now a smaller part of each frame's cost", fastSpeedup, fastSpeedup*base, rate, rate/base)
	}
	harness.Logger.Successf("✓ %.1f frames/s at 320 (%.2fx the default)", rate, rate/base)
	if err := expectDetectionsWithin(sv, jobID, sceneHD, 8, 10, 0.7); err != nil {
		return err
	}

	harness.Logger.Infof("With input_size: 300 (not a multiple of 32, the model's stride):")
	config := writeProfiles(harness, strings.Replace(perfProfilesYAML, "input_size: 320", "input_size: 300", 1))
	result, err := runCLI(harness, "analyze", sceneStreet.write(tempDir(harness)), "--config", config, "--profile", "fast")
	if err != nil {
		return err
	}
	if err := expectCleanFailure(result, "profiles.fast.input_size"); err != nil {
		return fmt.Errorf("with input_size: 300: %v", err)
	}
	harness.Logger.Successf("✓ exit code %d, stderr names profiles.fast.input_size", result.ExitCode)
	return nil
}

// speedupTarget is what n processes must reach against one: 1.9x for 4 cores.
func speedupTarget(n int) float64 { return 1 + 0.3*float64(n-1) }

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
	if n < 2 {
		return fmt.Errorf("this stage measures a speed-up across processes, and this machine (or its container's CPU quota) gives your program %d CPU. Run it where at least 2 are available: CPUS=2 or more for tools/run_tests.sh, or a pod with a CPU limit of 2", n)
	}
	tr := newTrace(harness)
	dbURL, env, err := workerStage(harness, map[string]string{"FRAMEWISE_TRACE": tr.path, "FRAMEWISE_DETECT_PROCESSES": "1",
		"FRAMEWISE_PROFILES": writeProfiles(harness, perfProfilesYAML)})
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
	singleRSS := sampleRSS(sv.workers[0].pid)
	_, single, _, err := timedJob(sv, tr, sceneHD, hdID)
	singlePeak, _ := singleRSS.finish()
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
	if len(pids) < n {
		return fmt.Errorf("expected the detector to run in %d processes at once, it ran in %d", n, len(pids))
	}
	if target := speedupTarget(n); rate < target*single {
		return fmt.Errorf("expected at least %.1fx the one-process rate with %d processes (%.0f frames/s), got %.0f (%.1fx). Profile the parent process: whatever it still does for every frame is your ceiling", target, n, target*single, rate, rate/single)
	}
	harness.Logger.Successf("✓ %.1fx faster with %d processes", rate/single, n)
	limit := singlePeak + int64(n)*perProcessMiB<<20
	if peak > limit {
		return fmt.Errorf("expected the worker and its processes to stay under %.0f MiB together (the one-process peak, %.0f MiB, plus %d MiB a process), they peaked at %.0f MiB. Frames waiting for a process take memory; so do frames nobody reads yet", mib(limit), mib(singlePeak), perProcessMiB, mib(peak))
	}
	harness.Logger.Successf("✓ peak %.0f MiB (limit %.0f)", mib(peak), mib(limit))
	count, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if want := len(expectedBoxes(sceneHD, 0, sceneHD.frames-1)); count != int64(want) {
		return fmt.Errorf("expected %d rows in results.detections for the job, found %d", want, count)
	}
	return expectDetectionsWithin(sv, jobID, sceneHD, 8, 10, 0.85)
}

// perProcessMiB is what each detection process may add to the worker: the
// model and its runtime's own memory, a few 1080p frames, not a backlog.
const perProcessMiB = 350

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
	harness.Logger.Infof("poison.avi kills the process that runs the model on frame 30")
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
