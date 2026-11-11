package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// profile is one named analysis setup, as the stage texts define it.
type profile struct {
	every   int
	maxGapS float64
	labels  []string
}

var allLabels = []string{"person", "car", "bicycle"}

// expectedLabels is what analyzing s with p must report: for each label p
// keeps that appears in a sampled frame, its presence segments.
func expectedLabels(s scene, p profile) map[string][]segment {
	want := map[string][]segment{}
	for _, label := range p.labels {
		if segs := groupFrames(s.presenceFrames(label, p.every), s.fps, p.maxGapS); len(segs) > 0 {
			want[label] = segs
		}
	}
	return want
}

// sampledFrames is how many frames sampling every p.every frames reads.
func sampledFrames(s scene, p profile) int { return (s.frames + p.every - 1) / p.every }

// labelsField reads {"person": [{"start_s", "end_s"}], ...} from object[key].
func labelsField(object map[string]any, key string) (map[string][]segment, error) {
	value, ok := object[key]
	if !ok {
		return nil, fmt.Errorf("expected a %q field, got %s", key, describeKeys(object))
	}
	inner, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`expected %q to be an object like {"person": [{"start_s": 1.0, "end_s": 8.0}]}, got %v`, key, value)
	}
	result := map[string][]segment{}
	for label := range inner {
		segs, err := segmentsField(inner, label)
		if err != nil {
			return nil, fmt.Errorf("%s.%v", key, err)
		}
		result[label] = segs
	}
	return result, nil
}

func formatLabels(labels map[string][]segment) string {
	if len(labels) == 0 {
		return "no labels"
	}
	names := make([]string, 0, len(labels))
	for name := range labels {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%s: %s", name, formatSegments(labels[name]))
	}
	return strings.Join(parts, "; ")
}

// compareLabels checks that got has exactly want's labels, each with segments
// whose boundaries are within one frame.
func compareLabels(got, want map[string][]segment, fps int) error {
	for label := range got {
		if _, ok := want[label]; !ok {
			return fmt.Errorf("expected labels %s, but got %q too (%s)", formatLabels(want), label, formatLabels(got))
		}
	}
	for label, segs := range want {
		g, ok := got[label]
		if !ok {
			return fmt.Errorf("expected %q in the labels (%s), got %s", label, formatLabels(want), formatLabels(got))
		}
		if err := compareSegments(g, segs, fps); err != nil {
			return fmt.Errorf("%s: %v", label, err)
		}
	}
	return nil
}

// A trace is the file fwdetect appends a line to on every call when
// FWDETECT_TRACE names it.
type trace struct{ path string }

type traceLine struct {
	PID   int     `json:"pid"`
	T     float64 `json:"t"`
	W     int     `json:"w"`
	H     int     `json:"h"`
	Boxes [][]any `json:"boxes"`
}

func newTrace(harness *test_case_harness.TestCaseHarness) trace {
	return trace{filepath.Join(tempDir(harness), "fwdetect-trace.jsonl")}
}

// lines returns every call so far; no file means no calls.
func (t trace) lines() ([]traceLine, error) {
	f, err := os.Open(t.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var result []traceLine
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var line traceLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return nil, fmt.Errorf("CodeCrafters Internal Error: bad fwdetect trace line %q", scanner.Text())
		}
		result = append(result, line)
	}
	return result, scanner.Err()
}

func (t trace) reset() { os.Remove(t.path) }
