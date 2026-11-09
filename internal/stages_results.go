package internal

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
	"github.com/jackc/pgx/v5"
)

// expectedBoxes lists every box the default profile stores for s in frames [from, to].
func expectedBoxes(s scene, from, to int) []box {
	var result []box
	for i := from; i <= to && i < s.frames; i++ {
		result = append(result, s.boxesAt(i)...)
	}
	return result
}

type storedBox struct {
	t float64
	box
}

func (b storedBox) String() string {
	return fmt.Sprintf("%.1f s %s (%d, %d, %d, %d)", b.t, b.Label, b.X, b.Y, b.W, b.H)
}

// detectionsField reads [{"t_s", "label", "x", "y", "w", "h"}] from object["detections"].
func detectionsField(object map[string]any) ([]storedBox, error) {
	list, ok := object["detections"].([]any)
	if !ok {
		return nil, fmt.Errorf(`expected {"detections": [...]}, got %s`, describeKeys(object))
	}
	var result []storedBox
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("detections[%d]: expected an object, got %v", i, item)
		}
		t, err := floatField(entry, "t_s")
		if err != nil {
			return nil, fmt.Errorf("detections[%d]: %v", i, err)
		}
		label, _ := entry["label"].(string)
		var xywh [4]int64
		for k, key := range []string{"x", "y", "w", "h"} {
			if xywh[k], err = intField(entry, key); err != nil {
				return nil, fmt.Errorf("detections[%d]: %v", i, err)
			}
		}
		result = append(result, storedBox{t, box{label, int(xywh[0]), int(xywh[1]), int(xywh[2]), int(xywh[3])}})
	}
	return result, nil
}

func sameBox(a, b box) bool {
	return a.Label == b.Label && abs(a.X-b.X) <= 2 && abs(a.Y-b.Y) <= 2 && abs(a.W-b.W) <= 2 && abs(a.H-b.H) <= 2
}

// compareDetections checks got against frames [from, to] of s, in order of time then label.
func compareDetections(got []storedBox, s scene, from, to int) error {
	var want []storedBox
	for i := from; i <= to && i < s.frames; i++ {
		boxes := s.boxesAt(i)
		sort.Slice(boxes, func(a, b int) bool { return boxes[a].Label < boxes[b].Label })
		for _, b := range boxes {
			want = append(want, storedBox{float64(i) / float64(s.fps), b})
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("expected %d detections, got %d", len(want), len(got))
	}
	for i := range want {
		if math.Abs(got[i].t-want[i].t) > 1e-3 || !sameBox(got[i].box, want[i].box) {
			return fmt.Errorf("expected detection %d (sorted by t_s, then label) to be %s, got %s (positions within 2 px)", i, want[i], got[i])
		}
	}
	return nil
}

func detectionCount(dbURL, jobID string) (int64, error) {
	rows, err := queryRows(dbURL, "SELECT count(*) FROM results.detections WHERE job_id = $1", jobID)
	if err != nil {
		return 0, fmt.Errorf("couldn't count your rows in results.detections (job_id, t_s, label, x, y, w, h): %v", err)
	}
	return rows[0][0].(int64), nil
}

// expectIndexScan checks that a time-range query for one job can use an index.
func expectIndexScan(harness *test_case_harness.TestCaseHarness, dbURL, jobID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "ANALYZE results.detections"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		return err
	}
	query := fmt.Sprintf("SELECT * FROM results.detections WHERE job_id = '%s' AND t_s BETWEEN 2 AND 4", jobID)
	rows, err := conn.Query(ctx, "EXPLAIN "+query)
	if err != nil {
		return fmt.Errorf("couldn't explain %q: %v", query, err)
	}
	var plan []string
	for rows.Next() {
		var line string
		rows.Scan(&line)
		plan = append(plan, line)
	}
	rows.Close()
	text := strings.Join(plan, "\n")
	harness.Logger.Infof("EXPLAIN %s\n%s", query, text)
	if strings.Contains(text, "Seq Scan") {
		return fmt.Errorf("expected that query to use an index (with enable_seqscan off), but Postgres can only scan the whole table. Add an index that serves it")
	}
	harness.Logger.Successf("✓ the query can use an index")
	return nil
}

func testStoreDetections(harness *test_case_harness.TestCaseHarness) error {
	dbURL, env, err := workerStage(harness, nil)
	if err != nil {
		return err
	}
	if err := expectTables(harness, dbURL, "results.detections"); err != nil {
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
	jobID, _, err := sv.submitJob(streetID, "")
	if err != nil {
		return err
	}
	if _, err := sv.expectJobDone(jobID, sceneStreet, defaultProfile); err != nil {
		return err
	}
	n, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if want := len(expectedBoxes(sceneStreet, 0, sceneStreet.frames-1)); n != int64(want) {
		return fmt.Errorf("expected one row in results.detections per box per analyzed frame (%d), found %d", want, n)
	}
	harness.Logger.Successf("✓ %d rows in results.detections", n)
	path := fmt.Sprintf("/jobs/%s/detections?from_s=2&to_s=4", jobID)
	r, err := sv.get(path, nil)
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
	if err := compareDetections(got, sceneStreet, 20, 40); err != nil {
		return err
	}
	harness.Logger.Successf("✓ %d detections between 2 and 4 s", len(got))
	r, err = sv.get("/jobs/"+randomID()+"/detections", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 404); err != nil {
		return fmt.Errorf("detections of an unknown job: %v", err)
	}
	harness.Logger.Successf("✓ 404 for an unknown job")
	return expectIndexScan(harness, dbURL, jobID)
}

const testEventsYAML = `# Written by the tester.
events:
  crossing:
    labels: [person, car]
    max_gap_s: 0.5
    min_duration_s: 0.15
  long_crossing:
    labels: [person, car]
    max_gap_s: 0.5
    min_duration_s: 1.0
`

type eventRule struct {
	name                  string
	a, b                  string
	maxGapS, minDurationS float64
}

var testEventRules = []eventRule{{"crossing", "person", "car", 0.5, 0.15}, {"long_crossing", "person", "car", 0.5, 1.0}}

type event struct {
	rule string
	segment
}

// expectedEvents applies the rules to s's overlap frames, minus the frames in removed.
func expectedEvents(s scene, removed map[int]bool) []event {
	var result []event
	for _, r := range testEventRules {
		var frames []int
		for _, f := range s.overlapFrames(r.a, r.b, 1) {
			if !removed[f] {
				frames = append(frames, f)
			}
		}
		for _, seg := range groupFrames(frames, s.fps, r.maxGapS) {
			if seg.End-seg.Start >= r.minDurationS-1e-9 {
				result = append(result, event{r.name, seg})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Start != result[j].Start {
			return result[i].Start < result[j].Start
		}
		return result[i].rule < result[j].rule
	})
	return result
}

func formatEvents(events []event) string {
	if len(events) == 0 {
		return "no events"
	}
	var parts []string
	for _, e := range events {
		parts = append(parts, fmt.Sprintf("%s %s", e.rule, e.segment))
	}
	return strings.Join(parts, ", ")
}

func (s *server) expectEvents(jobID string, sc scene, removed map[int]bool) error {
	r, err := s.get("/jobs/"+jobID+"/events", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 200); err != nil {
		return err
	}
	list, ok := r.object["events"].([]any)
	if !ok {
		return fmt.Errorf(`expected {"events": [...]}, got %s`, truncate(string(r.raw)))
	}
	var got []event
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("events[%d]: expected an object, got %v", i, item)
		}
		rule, _ := entry["rule"].(string)
		start, err := floatField(entry, "start_s")
		if err != nil {
			return fmt.Errorf("events[%d]: %v", i, err)
		}
		end, err := floatField(entry, "end_s")
		if err != nil {
			return fmt.Errorf("events[%d]: %v", i, err)
		}
		got = append(got, event{rule, segment{start, end}})
	}
	want := expectedEvents(sc, removed)
	if len(got) != len(want) {
		return fmt.Errorf("expected %s, got %s", formatEvents(want), formatEvents(got))
	}
	tolerance := 1/float64(sc.fps) + 1e-6
	for i := range want {
		if got[i].rule != want[i].rule || math.Abs(got[i].Start-want[i].Start) > tolerance || math.Abs(got[i].End-want[i].End) > tolerance {
			return fmt.Errorf("expected %s (sorted by start_s, then rule; boundaries within one frame), got %s", formatEvents(want), formatEvents(got))
		}
	}
	s.harness.Logger.Successf("✓ %s", formatEvents(got))
	return nil
}

func testEvents(harness *test_case_harness.TestCaseHarness) error {
	dbURL, env, err := workerStage(harness, map[string]string{"FRAMEWISE_EVENTS": writeEvents(harness)})
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	harness.Logger.Infof("Event rules:\n%s", testEventsYAML)
	streetID, err := sv.uploadClip("street.avi", sceneStreet.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(streetID, "")
	if err != nil {
		return err
	}
	if _, err := sv.expectJobDone(jobID, sceneStreet, defaultProfile); err != nil {
		return err
	}
	if err := sv.expectEvents(jobID, sceneStreet, nil); err != nil {
		return err
	}
	harness.Logger.Infof("Deleting the car's rows between 5.6 and 6.2 s from results.detections")
	if _, err := queryRows(dbURL, "DELETE FROM results.detections WHERE job_id = $1 AND label = 'car' AND t_s BETWEEN 5.55 AND 6.25", jobID); err != nil {
		return fmt.Errorf("couldn't delete from results.detections: %v", err)
	}
	removed := map[int]bool{}
	for f := 56; f <= 62; f++ {
		removed[f] = true
	}
	if err := sv.expectEvents(jobID, sceneStreet, removed); err != nil {
		return fmt.Errorf("after deleting rows: %v. Compute events from results.detections when asked, not from a copy made during the job", err)
	}
	r, err := sv.get("/jobs/"+randomID()+"/events", nil)
	if err != nil {
		return err
	}
	if err := expectStatus(r, 404); err != nil {
		return fmt.Errorf("events of an unknown job: %v", err)
	}
	harness.Logger.Successf("✓ 404 for an unknown job")
	return nil
}

func writeEvents(harness *test_case_harness.TestCaseHarness) string {
	return writeProfilesNamed(harness, "events", testEventsYAML)
}

func testRetries(harness *test_case_harness.TestCaseHarness) error {
	tr := newTrace(harness)
	extra := map[string]string{"FWDETECT_TRACE": tr.path, "FRAMEWISE_EVENTS": writeEvents(harness)}
	for k, v := range crashEnv {
		extra[k] = v
	}
	dbURL, env, err := workerStage(harness, extra)
	if err != nil {
		return err
	}
	sv, err := startService(harness, tempDir(harness), env, 1, false)
	if err != nil {
		return err
	}
	sv.supervise()
	longID, err := sv.uploadClip("long_street.avi", sceneLong.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := sv.submitJob(longID, "")
	if err != nil {
		return err
	}
	harness.Logger.Infof("Waiting for the job to store some detections")
	deadline := time.Now().Add(60 * time.Second)
	for {
		n, err := detectionCount(dbURL, jobID)
		if err != nil {
			return err
		}
		if n > 100 {
			harness.Logger.Successf("✓ %d rows so far", n)
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected detections to be stored while the job runs (in batches), but after 60 s there were %d rows", n)
		}
		time.Sleep(100 * time.Millisecond)
	}
	w, err := runningWorker(sv, tr)
	if err != nil {
		return err
	}
	w.kill()
	if _, err := sv.expectJobDone(jobID, sceneLong, defaultProfile); err != nil {
		return err
	}
	n, err := detectionCount(dbURL, jobID)
	if err != nil {
		return err
	}
	if want := len(expectedBoxes(sceneLong, 0, sceneLong.frames-1)); n != int64(want) {
		return fmt.Errorf("expected exactly %d rows in results.detections for the job after its retry, found %d. The first attempt's rows must not be counted twice", want, n)
	}
	harness.Logger.Successf("✓ exactly %d rows after the retry", n)
	return sv.expectEvents(jobID, sceneLong, nil)
}
