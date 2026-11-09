package internal

import (
	"bytes"
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

func expectVideo(object map[string]any, filename string, size int64) error {
	if object["filename"] != filename {
		return fmt.Errorf("expected filename %q, got %v", filename, object["filename"])
	}
	got, err := intField(object, "size_bytes")
	if err != nil {
		return err
	}
	if got != size {
		return fmt.Errorf("expected size_bytes %d, got %d", size, got)
	}
	return nil
}

// ap1's limits: uploads up to 200 MiB, and a 150 MiB upload may grow the
// server's memory by at most 60 MiB.
const (
	uploadLimitMiB   = 200
	bigUploadBytes   = 150 << 20
	uploadMaxGrowth  = 60 << 20
	uploadLimitBytes = uploadLimitMiB << 20
)

func testUpload(harness *test_case_harness.TestCaseHarness) error {
	dataDir := tempDir(harness)
	dbURL, err := prepareDatabase(harness, false)
	if err != nil {
		return err
	}
	s, err := jobServer(harness, dataDir, map[string]string{"FRAMEWISE_MAX_UPLOAD_MB": fmt.Sprint(uploadLimitMiB), "DATABASE_URL": dbURL})
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

	type upload struct {
		id, filename string
		size         int64
	}
	var uploads []upload
	for _, sc := range []scene{sceneStreet, scenePoison} {
		filename := fmt.Sprintf("%s-%s.AVI", sc.name, random.RandomWord())
		r, err := s.upload(filename, sc.bytes(), nil)
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
		if err := expectVideo(r.object, filename, int64(len(sc.bytes()))); err != nil {
			return err
		}
		harness.Logger.Successf("✓ 201, video_id %s", id)
		uploads = append(uploads, upload{id, filename, int64(len(sc.bytes()))})
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
	r, err = s.get("/videos/"+randomID(), nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 404); err != nil {
		return err
	}
	harness.Logger.Successf("✓ 404 for an unknown id")

	word := random.RandomWord()
	for _, name := range []string{word + ".txt", word} {
		r, err := s.upload(name, sceneStreet.bytes(), nil)
		if err != nil {
			return err
		}
		if err := expectStatus(r, 415); err != nil {
			return fmt.Errorf("%q: %v", name, err)
		}
		harness.Logger.Successf("✓ 415 for %q", name)
	}

	before := groupRSS(s.p.pid)
	sampler := sampleRSS(s.p.pid)
	r, err = s.uploadStream(word+".mp4", bigUploadBytes)
	peak, _ := sampler.finish()
	if err != nil {
		return err
	}
	if err := expectStatus(r, 201); err != nil {
		return fmt.Errorf("an upload under the limit: %v", err)
	}
	if growth := peak - before; growth > uploadMaxGrowth {
		return fmt.Errorf("expected a %.0f MiB upload to grow your server's memory by at most %.0f MiB, it grew by %.0f MiB. Write the upload to disk in chunks as it arrives", mib(bigUploadBytes), mib(uploadMaxGrowth), mib(growth))
	} else {
		harness.Logger.Successf("✓ 201, memory grew by %.0f MiB (limit %.0f MiB)", mib(growth), mib(uploadMaxGrowth))
	}

	r, err = s.uploadStream(word+".mkv", uploadLimitBytes+1)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 413); err != nil {
		return fmt.Errorf("an upload 1 byte over the %d MiB limit: %v", uploadLimitMiB, err)
	}
	harness.Logger.Successf("✓ 413 for 1 byte over the limit")
	var tooBig []string
	filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil && info.Size() > uploadLimitBytes {
				tooBig = append(tooBig, fmt.Sprintf("%s (%d bytes)", path, info.Size()))
			}
		}
		return nil
	})
	if len(tooBig) > 0 {
		return fmt.Errorf("expected nothing over the limit to be kept in --data-dir, found %v", tooBig)
	}
	harness.Logger.Successf("✓ nothing over the limit is kept in --data-dir")
	return nil
}

// jobServer starts a server with the tester's profiles and extra env.
func jobServer(harness *test_case_harness.TestCaseHarness, dataDir string, env map[string]string) (*server, error) {
	all := map[string]string{"FRAMEWISE_PROFILES": writeProfiles(harness, testProfilesYAML)}
	for k, v := range env {
		if v != "" {
			all[k] = v
		}
	}
	return startServer(harness, dataDir, all)
}

func testJobs(harness *test_case_harness.TestCaseHarness) error {
	dbURL, err := prepareDatabase(harness, false)
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), serviceEnv(harness, dbURL, nil), 1, true)
	if err != nil {
		return err
	}
	s := sv.server
	check := func(r response, err error, want int, what string) error {
		if err != nil {
			return err
		}
		if err := expectStatus(r, want); err != nil {
			return fmt.Errorf("%s: %v", what, err)
		}
		harness.Logger.Successf("✓ %d for %s", want, what)
		return nil
	}
	videoID, err := s.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	for _, run := range []struct{ requested, name string }{{"", "default"}, {"people", "people"}} {
		jobID, _, err := s.submitJob(videoID, run.requested)
		if err != nil {
			return err
		}
		job, err := s.expectJobDone(jobID, sceneStreet, testProfiles[run.name])
		if err != nil {
			return err
		}
		if job["video_id"] != videoID || job["profile"] != run.name || job["id"] != jobID {
			return fmt.Errorf(`expected the job to have "id" %q, "video_id" %q and "profile" %q, got %v, %v and %v`, jobID, videoID, run.name, job["id"], job["video_id"], job["profile"])
		}
	}

	r, err := s.get("/jobs/"+randomID(), nil)
	if err := check(r, err, 404, "an unknown job"); err != nil {
		return err
	}
	r, err = s.postJSON("/jobs", map[string]string{"video_id": randomID()})
	if err := check(r, err, 404, "an unknown video"); err != nil {
		return err
	}
	r, err = s.postJSON("/jobs", map[string]string{"profile": "default"})
	if err := check(r, err, 422, "no video_id"); err != nil {
		return err
	}
	r, err = s.postJSON("/jobs", map[string]string{"video_id": videoID, "profile": random.RandomWord()})
	if err := check(r, err, 422, "an unknown profile"); err != nil {
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

	return expectResponsive(harness, s, 3)
}

// expectResponsive submits n jobs on the long clip: each POST must answer
// within 2 s, before the analysis is done, and /health within 1 s meanwhile.
func expectResponsive(harness *test_case_harness.TestCaseHarness, s *server, n int) error {
	longID, err := s.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	var jobs []string
	for i := 0; i < n; i++ {
		body := fmt.Sprintf(`{"video_id": %q}`, longID)
		harness.Logger.Infof("POST /jobs %s", body)
		r, err := s.send("POST", "/jobs", "application/json", []byte(body), nil)
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
		if _, err := s.expectJobDone(id, sceneLong, defaultProfile); err != nil {
			return err
		}
	}
	return nil
}

func countRows(dbURL, table string) (int64, error) {
	rows, err := queryRows(dbURL, "SELECT count(*) FROM "+table)
	if err != nil {
		return 0, fmt.Errorf("couldn't count %s: %v", table, err)
	}
	return rows[0][0].(int64), nil
}

func testRestart(harness *test_case_harness.TestCaseHarness) error {
	dbURL, err := prepareDatabase(harness, true)
	if err != nil {
		return err
	}
	if err := expectTables(harness, dbURL, "queue.jobs", "results.videos"); err != nil {
		return err
	}
	dataDir := tempDir(harness)
	env := serviceEnv(harness, dbURL, nil)
	sv, err := startService(harness, dataDir, env, 1, true)
	if err != nil {
		return err
	}
	s := sv.server
	streetID, err := s.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	doneID, _, err := s.submitJob(streetID, "people")
	if err != nil {
		return err
	}
	done, err := s.expectJobDone(doneID, sceneStreet, testProfiles["people"])
	if err != nil {
		return err
	}
	longID, err := s.uploadClip("long_street.avi", sceneLong.bytes())
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

	for table, want := range map[string]int64{"results.videos": 2, "queue.jobs": 3} {
		n, err := countRows(dbURL, table)
		if err != nil {
			return err
		}
		if n != want {
			return fmt.Errorf("expected %d rows in %s after %d uploads and jobs, found %d. Keep videos and jobs in Postgres", want, table, want, n)
		}
	}
	harness.Logger.Successf("✓ results.videos has 2 rows, queue.jobs 3")

	harness.Logger.Infof("Starting the server again on the same database and --data-dir")
	s, err = startServer(harness, dataDir, env)
	if err != nil {
		return err
	}
	for _, id := range []string{streetID, longID} {
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
	before, _ := labelsField(done, "labels")
	after, err := labelsField(r.object, "labels")
	if err != nil {
		return err
	}
	if r.object["status"] != "DONE" || compareLabels(after, before, 1000) != nil {
		return fmt.Errorf("expected the finished job unchanged (DONE, %s), got %v, %s", formatLabels(before), r.object["status"], formatLabels(after))
	}
	harness.Logger.Successf("✓ DONE, %s, as before", formatLabels(after))
	for _, id := range pending {
		if _, err := s.expectJobDone(id, sceneLong, defaultProfile); err != nil {
			return fmt.Errorf("a job submitted before the restart: %v", err)
		}
	}
	return s.runJob(sceneStreet)
}
