package internal

import (
	"fmt"
	"time"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// From md1 the profiles file has profiles that run the model on the model server.
const remoteProfilesYAML = perfProfilesYAML + `  remote:
    every: 1
    max_gap_s: 0.5
    labels: [person, car, bicycle]
    detector: remote
  parked_remote:
    every: 2
    max_gap_s: 0.5
    model: yolo26n-cls
    labels: [sports_car, convertible]
    detector: remote
`

const remoteProfile = "remote"

// modelStage starts a model server whose yolo26n takes size x size images and
// prepares a database and environment whose profiles include "remote".
func modelStage(harness *test_case_harness.TestCaseHarness, size int, extra map[string]string) (string, map[string]string, *modelServer, error) {
	harness.Logger.Infof("Starting a model server (it runs your models/*.onnx with onnxruntime)")
	model, err := startModelServer(harness, size)
	if err != nil {
		return "", nil, nil, err
	}
	all := map[string]string{
		"FRAMEWISE_PROFILES":  writeProfiles(harness, remoteProfilesYAML),
		"FRAMEWISE_MODEL_URL": model.url(),
	}
	for k, v := range extra {
		all[k] = v
	}
	dbURL, env, err := workerStage(harness, all)
	if err != nil {
		return "", nil, nil, err
	}
	harness.Logger.Infof("A model server is at FRAMEWISE_MODEL_URL: models %q (input images [-1, 3, %d, %d]) and %q (GET /v2/models/<name>/config)", detectModel, size, size, classifyModel)
	return dbURL, env, model, nil
}

// expectRemoteDone is expectJobDone, with a hint when the boxes or labels look
// like the frame went to the model in the wrong layout or colour order.
func (sv *service) expectRemoteDone(jobID string, sc scene) (map[string]any, error) {
	job, err := sv.expectJobDone(jobID, sc, defaultProfile)
	if err == nil {
		return job, nil
	}
	if r, rerr := sv.get("/jobs/"+jobID, nil); rerr == nil && r.object != nil {
		if got, lerr := labelsField(r.object, "labels"); lerr == nil && len(got) == 0 {
			return nil, fmt.Errorf("%v. The model found nothing: check the input's layout (N, 3, H, W), its scale (0 to 1) and the order of its colours (RGB; OpenCV gives you BGR)", err)
		}
	}
	return nil, err
}

func (sv *service) workerAlive(what string) error {
	for i, w := range sv.workers {
		if w != nil && w.exited() {
			return fmt.Errorf("expected worker %d to survive %s, it exited with %d", i+1, what, w.result.ExitCode)
		}
	}
	sv.harness.Logger.Successf("✓ the worker is still running")
	return nil
}

func testRemoteDetector(harness *test_case_harness.TestCaseHarness) error {
	sizes := []int{512, 640}
	size := sizes[random.RandomInt(0, len(sizes))]
	tr := newTrace(harness)
	dbURL, env, model, err := modelStage(harness, size, tr.env())
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

	harness.Logger.Infof("A job with the remote profile on the 640x360 street clip")
	tr.reset()
	jobID, _, err := sv.submitJob(streetID, remoteProfile)
	if err != nil {
		return err
	}
	if _, err := sv.expectRemoteDone(jobID, sceneStreet); err != nil {
		return err
	}
	if calls, _ := tr.lines(); len(calls) > 0 {
		return fmt.Errorf("expected the remote profile to run the model on the model server only, but it ran on %d frames in your processes", len(calls))
	}
	stats := model.snapshot()
	want := sampledFrames(sceneStreet, defaultProfile)
	if stats.framesBy[detectModel] != want {
		return fmt.Errorf("expected the model server to get each of the %d frames once, it answered %d frames in %d requests (%d rejected)", want, stats.frames, stats.requests, stats.rejected)
	}
	harness.Logger.Successf("✓ the model server analyzed all %d frames, in %d requests; the model didn't run in your processes", stats.frames, stats.ok)
	r, err := sv.get(fmt.Sprintf("/jobs/%s/detections?from_s=2&to_s=4", jobID), nil)
	if err != nil {
		return err
	}
	got, err := detectionsField(r.object)
	if err != nil {
		return err
	}
	if err := compareDetectionsAt(got, sceneStreet, size, 20, 40, 0.85); err != nil {
		return fmt.Errorf("%v. The model answers in the pixels of its own input (%dx%d, letterboxed); store boxes in the video's", err, size, size)
	}
	harness.Logger.Successf("✓ %d detections between 2 and 4 s, in the video's coordinates", len(got))

	harness.Logger.Infof("A job with the remote profile on the 1920x1080 clip")
	hdID, err := sv.uploadClip("hd_street.avi", sceneHD.bytes())
	if err != nil {
		return err
	}
	model.resetStats()
	hdJob, _, err := sv.submitJob(hdID, remoteProfile)
	if err != nil {
		return err
	}
	if _, err := sv.expectRemoteDone(hdJob, sceneHD); err != nil {
		return err
	}
	if err := expectDetectionsWithin(sv, hdJob, sceneHD, size, 8, 10, 0.85); err != nil {
		return err
	}
	count, err := detectionCount(dbURL, hdJob)
	if err != nil {
		return err
	}
	if err := expectRowCount(count, sceneHD, size, 1, "the job"); err != nil {
		return err
	}

	harness.Logger.Infof("A job with the parked_remote profile (%s on the model server) on the parked clip", classifyModel)
	parkedID, err := sv.uploadClip("parked.avi", sceneParked.bytes())
	if err != nil {
		return err
	}
	model.resetStats()
	parkedJob, _, err := sv.submitJob(parkedID, "parked_remote")
	if err != nil {
		return err
	}
	parked := profile{every: 2, maxGapS: 0.5, labels: []string{"sports_car", "convertible"}, model: classifyModel}
	if _, err := sv.expectJobDone(parkedJob, sceneParked, parked); err != nil {
		return fmt.Errorf("%v. %s takes %dx%d images, resized (not letterboxed) from the middle of the frame: see models/README.md", err, classifyModel, classifySize, classifySize)
	}
	if st := model.snapshot(); st.framesBy[classifyModel] != sampledFrames(sceneParked, parked) {
		return fmt.Errorf("expected the model server to classify each of the %d sampled frames once, it classified %d", sampledFrames(sceneParked, parked), st.framesBy[classifyModel])
	}
	harness.Logger.Successf("✓ classified on the model server")

	harness.Logger.Infof("The default profile still runs the model in your worker")
	tr.reset()
	model.resetStats()
	localJob, _, err := sv.submitJob(streetID, "")
	if err != nil {
		return err
	}
	if _, err := sv.expectJobDone(localJob, sceneStreet, defaultProfile); err != nil {
		return err
	}
	calls, _ := tr.lines()
	if len(calls) != want || model.snapshot().requests > 0 {
		return fmt.Errorf("expected the default profile to run the model locally (%d frames) and not call the model server; it ran on %d frames locally and the model server got %d requests. Where the model runs is a setting of the profile", want, len(calls), model.snapshot().requests)
	}
	harness.Logger.Successf("✓ the model ran on %d frames locally; the model server wasn't called", len(calls))
	return nil
}

// md2's model server: every request costs batchLatency on top of running the
// model, and two run at once. The target is a multiple of what sending full
// batches one at a time could reach, measured on this machine before the job:
// only keeping both instances busy gets there.
const (
	batchLatency   = 300 * time.Millisecond
	batchMax       = 16
	batchInstances = 2
	batchOverlap   = 1.3
	batchMemoryMiB = 1000
)

func testBatching(harness *test_case_harness.TestCaseHarness) error {
	dbURL, env, model, err := modelStage(harness, 640, nil)
	if err != nil {
		return err
	}
	model.set(batchMax, batchInstances, batchLatency, 0)
	one, err := model.calibrate(1)
	if err != nil {
		return err
	}
	full, err := model.calibrate(batchMax)
	if err != nil {
		return err
	}
	single := 1 / (batchLatency + one).Seconds()
	serial := batchMax / (batchLatency + full).Seconds()
	target := batchOverlap * serial
	harness.Logger.Infof("Each request to the model costs %s plus running it (%.0f ms for one frame, %.0f ms for %d, on this machine); its config allows batches of %d and %d requests at once. One frame at a time would make %.1f frames/s, full batches one at a time %.1f; the target is %.1f", batchLatency, float64(one.Milliseconds()), float64(full.Milliseconds()), batchMax, batchMax, batchInstances, single, serial, target)
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	hdID, err := sv.uploadClip("hd_street.avi", sceneHD.bytes())
	if err != nil {
		return err
	}
	w := sv.workers[0]
	time.Sleep(3 * time.Second)
	idle := groupRSS(w.pid)
	limit := idle + batchMemoryMiB<<20
	harness.Logger.Infof("The idle worker uses %.0f MiB; the limit while it works is %.0f MiB", mib(idle), mib(limit))

	model.resetStats()
	jobID, _, err := sv.submitJob(hdID, remoteProfile)
	if err != nil {
		return err
	}
	harness.Logger.Infof("A job with the remote profile on the 1080p clip (%d frames)", sceneHD.frames)
	rss := sampleRSS(w.pid)
	started := time.Now()
	for {
		rows, err := queryRows(dbURL, "SELECT status FROM queue.jobs WHERE id = $1", jobID)
		if err != nil {
			rss.finish()
			return fmt.Errorf("couldn't read the job's status from queue.jobs: %v", err)
		}
		if status, _ := rows[0][0].(string); status == "DONE" || status == "FAILED" {
			break
		}
		rss.mu.Lock()
		peak := rss.peak
		rss.mu.Unlock()
		if peak > 3*limit {
			rss.finish()
			w.kill()
			return fmt.Errorf("expected the worker to stay under %.0f MiB, it reached %.0f MiB and the tester stopped it. Frames are being read faster than the model takes them, and nothing makes the reader wait", mib(limit), mib(peak))
		}
		// Too slow to make it: say why now rather than after minutes.
		if elapsed := time.Since(started); elapsed > 20*time.Second {
			if st := model.snapshot(); st.rate() < target/2 || elapsed > 150*time.Second {
				rss.finish()
				return fmt.Errorf("expected at least %.1f frames/s from the model server; after %.0f s it's %.1f (%d frames). %s", target, elapsed.Seconds(), st.rate(), st.frames, batchHint(st))
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	peak, at := rss.finish()
	if _, err := sv.expectRemoteDone(jobID, sceneHD); err != nil {
		return err
	}
	s := model.snapshot()
	harness.Logger.Infof("%.1f frames/s; %.1f frames a request on average; at most %d requests at once; the worker peaked at %.0f MiB", s.rate(), s.meanBatch(), s.maxInFlight, mib(peak))
	if s.rate() < target {
		return fmt.Errorf("expected at least %.1f frames/s from the model server (%.1fx full batches one at a time), got %.1f. %s", target, batchOverlap, s.rate(), batchHint(s))
	}
	harness.Logger.Successf("✓ %.1f frames/s (target %.1f)", s.rate(), target)
	if peak > limit {
		return fmt.Errorf("expected the worker to stay under %.0f MiB (idle plus %d MiB), it peaked at %.0f MiB after %.1f s. Frames waiting for the model take memory; bound how many are read ahead", mib(limit), batchMemoryMiB, mib(peak), at.Seconds())
	}
	harness.Logger.Successf("✓ peak %.0f MiB (limit %.0f)", mib(peak), mib(limit))
	count, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if err := expectRowCount(count, sceneHD, 640, 1, "the job"); err != nil {
		return err
	}
	harness.Logger.Successf("✓ %d rows in results.detections", count)
	return nil
}

func batchHint(s modelStats) string {
	switch {
	case s.meanBatch() < batchMax/2:
		return fmt.Sprintf("Your requests carry %.1f frames on average, and each one costs %s however few frames it carries (max_batch_size is in the model's config)", s.meanBatch(), batchLatency)
	case s.maxInFlight < batchInstances:
		return fmt.Sprintf("The model runs %d requests at once (instance_group in its config), and you send one at a time", batchInstances)
	}
	return "Is the worker reading and resizing frames while it waits for the model?"
}

func testModelFailures(harness *test_case_harness.TestCaseHarness) error {
	extra := map[string]string{"FRAMEWISE_MODEL_TIMEOUT_S": "2"}
	for k, v := range crashEnv {
		extra[k] = v
	}
	dbURL, env, model, err := modelStage(harness, 640, extra)
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
	poisonID, err := sv.uploadClip("poison.avi", scenePoison.bytes())
	if err != nil {
		return err
	}

	harness.Logger.Infof("The model server answers every third request with 503 (busy)")
	model.setFailEvery(3)
	jobID, _, err := sv.submitJob(streetID, remoteProfile)
	if err != nil {
		return err
	}
	if _, err := sv.expectRemoteDone(jobID, sceneStreet); err != nil {
		return fmt.Errorf("%v. A 503 means \"try again soon\"", err)
	}
	count, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if err := expectRowCount(count, sceneStreet, 640, 1, "the job"); err != nil {
		return fmt.Errorf("%v. A retried request's frames must be stored once", err)
	}
	harness.Logger.Successf("✓ DONE with %d rows, after %d answers of 503", count, model.snapshot().busy)
	model.setFailEvery(0)

	failed := func(what, id, hint string) error {
		job, err := sv.waitStatus(id, 45*time.Second, "FAILED", "DONE")
		if err != nil {
			return fmt.Errorf("%s: %v. %s", what, err, hint)
		}
		if job["status"] != "FAILED" {
			return fmt.Errorf("%s: expected the job to end FAILED, it is %v", what, job["status"])
		}
		message, _ := job["error"].(string)
		if message == "" {
			return fmt.Errorf("%s: expected the FAILED job to say why in \"error\"", what)
		}
		harness.Logger.Successf("✓ FAILED: %s", message)
		return sv.workerAlive(what)
	}

	harness.Logger.Infof("poison.avi: the model fails with 500 on the batch holding frame 30")
	model.resetStats()
	id, _, err := sv.submitJob(poisonID, remoteProfile)
	if err != nil {
		return err
	}
	if err := failed("the poison clip", id, "The model answers 500 for the batch holding the grey frame, every time"); err != nil {
		return err
	}
	harness.Logger.Infof("(the model answered 500 %d times)", model.snapshot().failed)

	harness.Logger.Infof("The model server goes away: connections are refused")
	model.goAway()
	if id, _, err = sv.submitJob(streetID, remoteProfile); err != nil {
		return err
	}
	if err := failed("a model server that's down", id, "Retry a few times with a backoff, then give up with a clear error"); err != nil {
		return err
	}

	harness.Logger.Infof("The model server comes back, but accepts requests and never answers them (FRAMEWISE_MODEL_TIMEOUT_S is 2)")
	if err := model.comeBack(); err != nil {
		return err
	}
	model.setHanging(true)
	if id, _, err = sv.submitJob(streetID, remoteProfile); err != nil {
		return err
	}
	if err := failed("a model server that never answers", id, "Without a timeout your worker waits forever, and its heartbeat keeps the lease alive while it does"); err != nil {
		return err
	}

	harness.Logger.Infof("The model server is healthy again")
	model.setHanging(false)
	if id, _, err = sv.submitJob(streetID, remoteProfile); err != nil {
		return err
	}
	if _, err := sv.expectRemoteDone(id, sceneStreet); err != nil {
		return err
	}
	if model.snapshot().ok == 0 {
		return fmt.Errorf("expected the last job to go through the model server")
	}
	return nil
}
