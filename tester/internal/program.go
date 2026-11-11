package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/codecrafters-io/tester-utils/executable"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

type segment struct{ Start, End float64 }

func (s segment) String() string { return fmt.Sprintf("%.2f-%.2f s", s.Start, s.End) }

func formatSegments(segments []segment) string {
	if len(segments) == 0 {
		return "no segments"
	}
	parts := make([]string, len(segments))
	for i, s := range segments {
		parts[i] = s.String()
	}
	return strings.Join(parts, ", ")
}

// tempDir is removed when the stage ends.
func tempDir(harness *test_case_harness.TestCaseHarness) string {
	dir, err := os.MkdirTemp("", "framewise-")
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
	harness.RegisterTeardownFunc(func() { os.RemoveAll(dir) })
	return dir
}

// runCLI runs `./your_program.sh args...`, logging the command line.
func runCLI(harness *test_case_harness.TestCaseHarness, args ...string) (executable.ExecutableResult, error) {
	harness.Logger.Infof("$ ./your_program.sh %s", strings.Join(args, " "))
	exe := harness.NewExecutable()
	exe.TimeoutInMilliseconds = 60 * 1000
	result, err := exe.Run(args...)
	if err != nil {
		return result, fmt.Errorf("couldn't run your program: %v", err)
	}
	return result, nil
}

// jsonObject parses stdout as exactly one JSON object, keeping numbers as written.
func jsonObject(stdout []byte) (map[string]any, error) {
	text := bytes.TrimSpace(stdout)
	if len(text) == 0 {
		return nil, fmt.Errorf("expected a JSON object on stdout, got nothing")
	}
	decoder := json.NewDecoder(bytes.NewReader(text))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("expected stdout to be one JSON object, got %q (%v). Send debug output to stderr", truncate(string(text)), err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("expected stdout to be one JSON object and nothing else, got %q. Send debug output to stderr", truncate(string(text)))
	}
	return object, nil
}

func truncate(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}

// intField reads an integer field. 40.0 is not an integer here.
func intField(object map[string]any, key string) (int64, error) {
	value, ok := object[key]
	if !ok {
		return 0, fmt.Errorf("expected a %q field, got %s", key, describeKeys(object))
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("expected %q to be an integer, got %v", key, value)
	}
	n, err := number.Int64()
	if err != nil {
		return 0, fmt.Errorf("expected %q to be an integer, got %s", key, number)
	}
	return n, nil
}

func floatField(object map[string]any, key string) (float64, error) {
	value, ok := object[key]
	if !ok {
		return 0, fmt.Errorf("expected a %q field, got %s", key, describeKeys(object))
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("expected %q to be a number, got %v", key, value)
	}
	return number.Float64()
}

func describeKeys(object map[string]any) string {
	keys := make([]string, 0, len(object))
	for k := range object {
		keys = append(keys, fmt.Sprintf("%q", k))
	}
	if len(keys) == 0 {
		return "an object with no fields"
	}
	return "fields " + strings.Join(keys, ", ")
}

// segmentsField reads [{"start_s": ..., "end_s": ...}, ...] from object[key].
func segmentsField(object map[string]any, key string) ([]segment, error) {
	value, ok := object[key]
	if !ok {
		return nil, fmt.Errorf("expected a %q field, got %s", key, describeKeys(object))
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected %q to be a list, got %v", key, value)
	}
	var result []segment
	for i, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected %s[%d] to be an object with start_s and end_s, got %v", key, i, item)
		}
		start, err := floatField(entry, "start_s")
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %v", key, i, err)
		}
		end, err := floatField(entry, "end_s")
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %v", key, i, err)
		}
		result = append(result, segment{start, end})
	}
	return result, nil
}

// compareSegments allows each boundary to be off by one frame.
func compareSegments(got, want []segment, fps int) error {
	if len(got) != len(want) {
		return fmt.Errorf("expected %d segment(s) (%s), got %d (%s)", len(want), formatSegments(want), len(got), formatSegments(got))
	}
	tolerance := 1/float64(fps) + 1e-6
	for i := range want {
		if math.Abs(got[i].Start-want[i].Start) > tolerance || math.Abs(got[i].End-want[i].End) > tolerance {
			return fmt.Errorf("expected segment %d to be %s (each boundary within one frame, %.3f s), got %s", i+1, want[i], tolerance, got[i])
		}
	}
	return nil
}

// expectCleanFailure is the contract for bad input: a non-zero exit,
// the given text in stderr, nothing on stdout and no traceback.
func expectCleanFailure(result executable.ExecutableResult, mustMention string) error {
	if result.ExitCode == 0 {
		return fmt.Errorf("expected a non-zero exit code, got 0")
	}
	if strings.Contains(string(result.Stderr), "Traceback") {
		return fmt.Errorf("expected a clean error message, but stderr has a Python traceback. Catch the error and print one line instead")
	}
	if !strings.Contains(string(result.Stderr), mustMention) {
		return fmt.Errorf("expected stderr to mention %q, got %q", mustMention, truncate(strings.TrimSpace(string(result.Stderr))))
	}
	if out := strings.TrimSpace(string(result.Stdout)); out != "" {
		return fmt.Errorf("expected nothing on stdout, got %q", truncate(out))
	}
	return nil
}

// runCLIWithEnv is runCLI with extra environment variables, shown in the log.
func runCLIWithEnv(harness *test_case_harness.TestCaseHarness, env map[string]string, args ...string) (executable.ExecutableResult, error) {
	var shown []string
	for k := range env {
		shown = append(shown, k+"=...")
	}
	harness.Logger.Infof("$ %s./your_program.sh %s", strings.Join(append(shown, ""), " "), strings.Join(args, " "))
	exe := harness.NewExecutable()
	exe.TimeoutInMilliseconds = 120 * 1000
	var (
		result executable.ExecutableResult
		err    error
	)
	withEnv(env, func() { result, err = exe.Run(args...) })
	if err != nil {
		return result, fmt.Errorf("couldn't run your program: %v", err)
	}
	return result, nil
}
