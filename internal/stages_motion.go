package internal

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

const defaultMaxGapS = 0.5

func testMotionAny(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	// One segment from the first motion frame to the last.
	for _, c := range []clip{clipSquare, clipStill, clipBig} {
		if err := expectSegments(harness, c, c.write(dir), math.Inf(1)); err != nil {
			return err
		}
	}
	return nil
}

func testMotionSegments(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	// A segment is a run of consecutive motion frames.
	for _, c := range []clip{clipTwoMoves, clipSquare} {
		if err := expectSegments(harness, c, c.write(dir), 1/float64(c.fps)); err != nil {
			return err
		}
	}
	return nil
}

func testMotionGap(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, c := range []clip{clipPause, clipTwoMoves} {
		if err := expectSegments(harness, c, c.write(dir), defaultMaxGapS); err != nil {
			return err
		}
	}
	return nil
}

func testMotionNoise(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	for _, c := range []clip{clipNoisyStill, clipNoisySquare, clipSquare} {
		if err := expectSegments(harness, c, c.write(dir), defaultMaxGapS); err != nil {
			return err
		}
	}
	return nil
}

func writeConfig(harness *test_case_harness.TestCaseHarness, dir, contents string) string {
	path := filepath.Join(dir, fmt.Sprintf("detectors-%s.yaml", random.RandomWord()))
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
	harness.Logger.Infof("%s:", filepath.Base(path))
	for _, line := range splitLines(contents) {
		harness.Logger.Plainln("  " + line)
	}
	return path
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func testConfigLoad(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	words := random.RandomWords(3)
	blind, keen, split := words[0], words[1], words[2]

	config := writeConfig(harness, dir, fmt.Sprintf(`max_gap_s: 0.5
detectors:
  %s:
    threshold: 25
    min_area: 1000
  %s:
    threshold: 25
    min_area: 50
`, blind, keen))
	square := clipSquare.write(dir)
	got, err := analyze(harness, square, "--config", config, "--detector", blind)
	if err != nil {
		return err
	}
	if len(got) != 0 {
		return fmt.Errorf("with min_area: 1000, expected no segments, got %s", formatSegments(got))
	}
	harness.Logger.Successf("✓ no segments")
	if err := expectSegments(harness, clipSquare, square, defaultMaxGapS, "--config", config, "--detector", keen); err != nil {
		return err
	}

	config = writeConfig(harness, dir, fmt.Sprintf(`max_gap_s: 0.25
detectors:
  %s:
    threshold: 25
    min_area: 20
`, split))
	return expectSegments(harness, clipPause, clipPause.write(dir), 0.25, "--config", config, "--detector", split)
}

func testConfigInvalid(harness *test_case_harness.TestCaseHarness) error {
	dir := tempDir(harness)
	square := clipSquare.write(dir)
	name := random.RandomWord()

	cases := []struct {
		yaml, detector, mention string
	}{
		{fmt.Sprintf("detectors:\n  %s:\n    threshold: -5\n    min_area: 20\n", name), name, "threshold"},
		{fmt.Sprintf("detectors:\n  %s:\n    threshold: 300\n    min_area: 20\n", name), name, "threshold"},
		{fmt.Sprintf("detectors:\n  %s:\n    threshold: 25\n    min_area: lots\n", name), name, "min_area"},
	}
	unknown := random.RandomWord() + "-" + random.RandomWord()
	cases = append(cases, struct{ yaml, detector, mention string }{
		fmt.Sprintf("detectors:\n  %s:\n    threshold: 25\n    min_area: 20\n", name), unknown, unknown,
	})
	for _, tc := range cases {
		config := writeConfig(harness, dir, tc.yaml)
		result, err := runCLI(harness, "analyze", square, "--config", config, "--detector", tc.detector)
		if err != nil {
			return err
		}
		if err := expectCleanFailure(result, tc.mention); err != nil {
			return err
		}
		harness.Logger.Successf("✓ exit code %d, stderr mentions %q", result.ExitCode, tc.mention)
	}

	missing := filepath.Join(dir, fmt.Sprintf("missing-%s.yaml", random.RandomWord()))
	result, err := runCLI(harness, "analyze", square, "--config", missing)
	if err != nil {
		return err
	}
	if err := expectCleanFailure(result, filepath.Base(missing)); err != nil {
		return err
	}
	harness.Logger.Successf("✓ exit code %d, stderr names the file", result.ExitCode)
	return nil
}
