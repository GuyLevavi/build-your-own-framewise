package internal

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

func randomID() string {
	return fmt.Sprintf("%s-%d", random.RandomWord(), random.RandomInt(100000, 999999))
}

func testServeHealth(harness *test_case_harness.TestCaseHarness) error {
	s, err := startServer(harness, tempDir(harness), nil)
	if err != nil {
		return err
	}
	r, err := s.get("/health", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return err
	}
	if r.object["status"] != "ok" || len(r.object) != 1 {
		return fmt.Errorf(`expected {"status": "ok"}, got %s`, truncate(string(r.raw)))
	}
	harness.Logger.Successf(`✓ 200 {"status": "ok"}`)
	return nil
}

func testUpload(harness *test_case_harness.TestCaseHarness) error {
	s, err := startServer(harness, tempDir(harness), nil)
	if err != nil {
		return err
	}
	type upload struct {
		id, filename string
		size         int
	}
	var uploads []upload
	for _, c := range []clip{clipSquare, clipBig} {
		filename := fmt.Sprintf("%s-%s.avi", c.name, random.RandomWord())
		r, err := s.upload(filename, c.bytes(), nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 201); err != nil {
			return err
		}
		id, err := stringField(r.object, "video_id")
		if err != nil {
			return err
		}
		if err := expectVideo(r.object, filename, len(c.bytes())); err != nil {
			return err
		}
		harness.Logger.Successf("✓ 201, video_id %s", id)
		uploads = append(uploads, upload{id, filename, len(c.bytes())})
	}
	if uploads[0].id == uploads[1].id {
		return fmt.Errorf("expected every upload to get its own video_id, got %s twice", uploads[0].id)
	}
	for _, u := range uploads {
		r, err := s.get("/videos/"+u.id, nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 200); err != nil {
			return err
		}
		if err := expectVideo(r.object, u.filename, u.size); err != nil {
			return err
		}
		if r.object["video_id"] != u.id {
			return fmt.Errorf("expected video_id %q, got %v", u.id, r.object["video_id"])
		}
		harness.Logger.Successf("✓ 200, %s, %d bytes", u.filename, u.size)
	}
	r, err := s.get("/videos/"+randomID(), nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 404); err != nil {
		return err
	}
	harness.Logger.Successf("✓ 404 for an unknown id")
	return nil
}

func expectVideo(object map[string]any, filename string, size int) error {
	if object["filename"] != filename {
		return fmt.Errorf("expected filename %q, got %v", filename, object["filename"])
	}
	got, err := intField(object, "size_bytes")
	if err != nil {
		return err
	}
	if got != int64(size) {
		return fmt.Errorf("expected size_bytes %d, got %d", size, got)
	}
	return nil
}

func testJob(harness *test_case_harness.TestCaseHarness) error {
	s, err := startServer(harness, tempDir(harness), nil)
	if err != nil {
		return err
	}
	for _, run := range []struct {
		c        clip
		detector string
	}{{clipSquare, ""}, {clipTwoMoves, "default"}} {
		videoID, err := s.uploadClip(run.c.name+".avi", run.c.bytes())
		if err != nil {
			return err
		}
		jobID, _, err := s.submitJob(videoID, run.detector)
		if err != nil {
			return err
		}
		job, err := s.expectJobDone(jobID, run.c)
		if err != nil {
			return err
		}
		if job["video_id"] != videoID || job["detector"] != "default" || job["id"] != jobID {
			return fmt.Errorf(`expected the job to have "id" %q, "video_id" %q and "detector" "default", got %v, %v and %v`, jobID, videoID, job["id"], job["video_id"], job["detector"])
		}
	}
	return nil
}

func testJobErrors(harness *test_case_harness.TestCaseHarness) error {
	s, err := startServer(harness, tempDir(harness), nil)
	if err != nil {
		return err
	}
	check := func(r response, err error, want int, what string) error {
		if err != nil {
			return err
		}
		if err := expectStatus(r, want); err != nil {
			return fmt.Errorf("%s: %v", what, err)
		}
		harness.Logger.Successf("✓ %d", want)
		return nil
	}
	r, err := s.get("/jobs/"+randomID(), nil)
	if err := check(r, err, 404, "unknown job"); err != nil {
		return err
	}
	r, err = s.postJSON("/jobs", map[string]string{"video_id": randomID()})
	if err := check(r, err, 404, "unknown video"); err != nil {
		return err
	}
	r, err = s.postJSON("/jobs", map[string]string{"detector": "default"})
	if err := check(r, err, 422, "no video_id"); err != nil {
		return err
	}
	videoID, err := s.uploadClip("square.avi", clipSquare.bytes())
	if err != nil {
		return err
	}
	r, err = s.postJSON("/jobs", map[string]string{"video_id": videoID, "detector": random.RandomWord()})
	if err := check(r, err, 422, "unknown detector"); err != nil {
		return err
	}

	garbage := bytes.Repeat([]byte("definitely not a video\n"), 100)
	badID, err := s.uploadClip(random.RandomWord()+".mp4", garbage)
	if err != nil {
		return err
	}
	jobID, _, err := s.submitJob(badID, "")
	if err != nil {
		return err
	}
	job, err := s.waitJob(jobID, 30*time.Second)
	if err != nil {
		return err
	}
	if job["status"] != "FAILED" {
		return fmt.Errorf("expected a job on a file that isn't a video to be FAILED, got %v", job["status"])
	}
	if message, _ := job["error"].(string); message == "" {
		return fmt.Errorf("expected a FAILED job to have a non-empty \"error\", got %v", job["error"])
	}
	harness.Logger.Successf("✓ FAILED: %v", job["error"])
	return s.runJob(clipSquare)
}

func testBackground(harness *test_case_harness.TestCaseHarness) error {
	s, err := startServer(harness, tempDir(harness), nil)
	if err != nil {
		return err
	}
	videoID, err := s.uploadClip("long.avi", clipLong.bytes())
	if err != nil {
		return err
	}
	var jobs []string
	for i := 0; i < 3; i++ {
		harness.Logger.Infof("POST /jobs {\"video_id\": %q}", videoID)
		r, err := s.send("POST", "/jobs", "application/json", []byte(fmt.Sprintf(`{"video_id": %q}`, videoID)), nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 202); err != nil {
			return err
		}
		if r.took > 2*time.Second {
			return fmt.Errorf("expected POST /jobs to answer within 2 s, it took %.1f s. Run the analysis in the background", r.took.Seconds())
		}
		status, _ := r.object["status"].(string)
		if status != "PENDING" && status != "RUNNING" {
			return fmt.Errorf("expected POST /jobs to answer with status PENDING or RUNNING, before the analysis is done, got %v", r.object["status"])
		}
		id, err := stringField(r.object, "id")
		if err != nil {
			return err
		}
		harness.Logger.Successf("✓ 202 in %.2f s, %s", r.took.Seconds(), status)
		jobs = append(jobs, id)
	}
	r, err := s.get("/health", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return err
	}
	if r.took > time.Second {
		return fmt.Errorf("expected GET /health to answer within 1 s while jobs run, it took %.1f s", r.took.Seconds())
	}
	harness.Logger.Successf("✓ 200 in %.2f s while jobs run", r.took.Seconds())
	for _, id := range jobs {
		if _, err := s.expectJobDone(id, clipLong); err != nil {
			return err
		}
	}
	return nil
}

func testRestart(harness *test_case_harness.TestCaseHarness) error {
	dataDir := tempDir(harness)
	s, err := startServer(harness, dataDir, nil)
	if err != nil {
		return err
	}
	squareID, err := s.uploadClip("square.avi", clipSquare.bytes())
	if err != nil {
		return err
	}
	doneID, _, err := s.submitJob(squareID, "")
	if err != nil {
		return err
	}
	done, err := s.expectJobDone(doneID, clipSquare)
	if err != nil {
		return err
	}
	longID, err := s.uploadClip("long.avi", clipLong.bytes())
	if err != nil {
		return err
	}
	var pending []string
	for i := 0; i < 2; i++ {
		id, _, err := s.submitJob(longID, "")
		if err != nil {
			return err
		}
		pending = append(pending, id)
	}
	s.stop()

	harness.Logger.Infof("Starting the server again on the same --data-dir")
	s, err = startServer(harness, dataDir, nil)
	if err != nil {
		return err
	}
	for _, id := range []string{squareID, longID} {
		r, err := s.get("/videos/"+id, nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 200); err != nil {
			return fmt.Errorf("an upload from before the restart: %v", err)
		}
		harness.Logger.Successf("✓ 200, still there")
	}
	r, err := s.get("/jobs/"+doneID, nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return fmt.Errorf("a job finished before the restart: %v", err)
	}
	before, _ := segmentsField(done, "segments")
	after, err := segmentsField(r.object, "segments")
	if err != nil {
		return err
	}
	if r.object["status"] != "DONE" || compareSegments(after, before, 1000) != nil {
		return fmt.Errorf("expected the finished job unchanged (DONE, %s), got %v, %s", formatSegments(before), r.object["status"], formatSegments(after))
	}
	harness.Logger.Successf("✓ DONE, %s, as before", formatSegments(after))
	for _, id := range pending {
		if _, err := s.expectJobDone(id, clipLong); err != nil {
			return fmt.Errorf("a job submitted before the restart: %v", err)
		}
	}
	return s.runJob(clipTwoMoves)
}

func testAPIKey(harness *test_case_harness.TestCaseHarness) error {
	key := fmt.Sprintf("%s-%s-%d", random.RandomWord(), random.RandomWord(), random.RandomInt(1000, 9999))
	s, err := startServer(harness, tempDir(harness), map[string]string{"FRAMEWISE_API_KEY": key})
	if err != nil {
		return err
	}
	r, err := s.get("/health", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return fmt.Errorf("GET /health needs no key: %v", err)
	}
	harness.Logger.Successf("✓ 200 without a key")

	jobPath := "/jobs/" + randomID()
	for _, attempt := range []struct{ what, value string }{
		{"no key", ""},
		{"a wrong key", random.RandomWord()},
		{"half the key", key[:len(key)/2]},
		{"the key plus one character", key + "x"},
	} {
		harness.Logger.Infof("GET %s with %s", jobPath, attempt.what)
		headers := map[string]string{}
		if attempt.value != "" {
			headers["X-API-Key"] = attempt.value
		}
		r, err := s.send("GET", jobPath, "", nil, headers)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 401); err != nil {
			return fmt.Errorf("with %s: %v", attempt.what, err)
		}
		harness.Logger.Successf("✓ 401")
	}
	harness.Logger.Infof("GET %s with the right key", jobPath)
	r, err = s.send("GET", jobPath, "", nil, map[string]string{"X-API-Key": key})
	if err != nil {
		return err
	}
	if err := expectStatus(r, 404); err != nil {
		return fmt.Errorf("with the right key: %v", err)
	}
	harness.Logger.Successf("✓ 404")

	r, err = s.upload("square.avi", clipSquare.bytes(), nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 401); err != nil {
		return fmt.Errorf("an upload without a key: %v", err)
	}
	harness.Logger.Successf("✓ 401")

	harness.Logger.Infof("Running a job, sending the key")
	s.apiKey = key
	return s.runJob(clipSquare)
}

func testUploadLimits(harness *test_case_harness.TestCaseHarness) error {
	dataDir := tempDir(harness)
	s, err := startServer(harness, dataDir, map[string]string{"FRAMEWISE_MAX_UPLOAD_MB": "1"})
	if err != nil {
		return err
	}
	const mib = 1024 * 1024
	noise := func(n int) []byte {
		b := make([]byte, n)
		rand.Read(b)
		return b
	}
	word := random.RandomWord()
	videoName := word + ".AVI"
	cases := []struct {
		filename string
		data     []byte
		want     int
	}{
		{word + ".txt", []byte("not a video\n"), 415},
		{word, clipSquare.bytes(), 415},
		{word + ".mp4", noise(mib), 201},
		{word + ".mp4", noise(mib + 1), 413},
		{word + ".mkv", noise(5 * mib), 413},
		{videoName, clipSquare.bytes(), 201},
	}
	var videoID string
	for _, tc := range cases {
		r, err := s.upload(tc.filename, tc.data, nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, tc.want); err != nil {
			return fmt.Errorf("%s (%d bytes): %v", tc.filename, len(tc.data), err)
		}
		harness.Logger.Successf("✓ %d", tc.want)
		if tc.filename == videoName {
			videoID, _ = r.object["video_id"].(string)
		}
	}
	var tooBig []string
	filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil && info.Size() > mib {
				tooBig = append(tooBig, fmt.Sprintf("%s (%d bytes)", path, info.Size()))
			}
		}
		return nil
	})
	if len(tooBig) > 0 {
		return fmt.Errorf("expected nothing over the limit to be stored, found %v", tooBig)
	}
	harness.Logger.Successf("✓ nothing over the limit is stored in --data-dir")

	jobID, _, err := s.submitJob(videoID, "")
	if err != nil {
		return err
	}
	_, err = s.expectJobDone(jobID, clipSquare)
	return err
}
