package internal

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// probe runs `probe path` and returns the parsed JSON object.
func probe(harness *test_case_harness.TestCaseHarness, path string) (map[string]any, error) {
	result, err := runCLI(harness, "probe", path)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	return jsonObject(result.Stdout)
}

func expectProbe(object map[string]any, s scene) error {
	for _, f := range []struct {
		key  string
		want int
	}{{"width", s.width}, {"height", s.height}, {"frame_count", s.frames}} {
		got, err := intField(object, f.key)
		if err != nil {
			return err
		}
		if got != int64(f.want) {
			return fmt.Errorf("expected %s %d, got %d", f.key, f.want, got)
		}
	}
	fps, err := floatField(object, "fps")
	if err != nil {
		return err
	}
	if math.Abs(fps-float64(s.fps)) > 0.01 {
		return fmt.Errorf("expected fps %d (within 0.01), got %v", s.fps, fps)
	}
	duration, err := floatField(object, "duration_s")
	if err != nil {
		return err
	}
	if math.Abs(duration-s.duration()) > 0.01 {
		return fmt.Errorf("expected duration_s %.2f (frame_count / fps, within 0.01), got %v", s.duration(), duration)
	}
	return nil
}

func testProbe(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, s := range []scene{sceneStreet, scenePoison} {
		object, err := probe(harness, s.write(dir))
		if err != nil {
			return err
		}
		if err := expectProbe(object, s); err != nil {
			return err
		}
		harness.Logger.Successf("✓ %dx%d, %d fps, %d frames, %.1f s", s.width, s.height, s.fps, s.frames, s.duration())
	}
	return nil
}

func testProbeErrors(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	bad := []struct {
		prefix, ext string
		data        []byte
	}{
		{"missing", ".mp4", nil},
		{"garbage", ".mp4", []byte(strings.Repeat("this is not a video, just some text\n", 40))},
		{"empty", ".avi", []byte{}},
		{"truncated", ".avi", sceneStreet.bytes()[:300]},
	}
	for _, b := range bad {
		path := filepath.Join(dir, fmt.Sprintf("%s-%s%s", b.prefix, random.RandomWord(), b.ext))
		if b.data != nil {
			if err := os.WriteFile(path, b.data, 0o644); err != nil {
				return err
			}
		}
		result, err := runCLI(harness, "probe", path)
		if err != nil {
			return err
		}
		if err := expectCleanFailure(result, filepath.Base(path)); err != nil {
			return fmt.Errorf("%s file: %v", b.prefix, err)
		}
		harness.Logger.Successf("✓ %s file: exit code %d, stderr names the file", b.prefix, result.ExitCode)
	}
	return nil
}
