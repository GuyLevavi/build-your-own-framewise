package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/executable"
	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

var defaultProfile = profile{every: 1, maxGapS: 0.5, labels: allLabels}

// withEnv sets env for the duration of f (programs inherit the tester's environment).
func withEnv(env map[string]string, f func()) {
	for k, v := range env {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range env {
			os.Unsetenv(k)
		}
	}()
	f()
}

// runMeasured runs the program like runCLI and also samples its peak memory.
func runMeasured(harness *test_case_harness.TestCaseHarness, env map[string]string, args ...string) (executable.ExecutableResult, int64, time.Duration, error) {
	harness.Logger.Infof("$ ./your_program.sh %s", strings.Join(args, " "))
	exe := harness.NewExecutable()
	exe.TimeoutInMilliseconds = 180 * 1000
	var (
		result  executable.ExecutableResult
		peak    int64
		elapsed time.Duration
		err     error
	)
	withEnv(env, func() {
		start := time.Now()
		if err = exe.Start(args...); err != nil {
			err = fmt.Errorf("couldn't run your program: %v", err)
			return
		}
		sampler := sampleRSS(exe.Process.Pid)
		result, err = exe.Wait()
		peak, _ = sampler.finish()
		elapsed = time.Since(start)
	})
	return result, peak, elapsed, err
}

// analyzeScene runs `analyze <s> args...` with a fresh fwdetect trace and checks
// the labels against p and the number of detector calls against p.every.
func analyzeScene(harness *test_case_harness.TestCaseHarness, s scene, p profile, maxRSS int64, args ...string) error {
	path := s.write(tempDir(harness))
	tr := newTrace(harness)
	result, peak, elapsed, err := runMeasured(harness, map[string]string{"FWDETECT_TRACE": tr.path}, append([]string{"analyze", path}, args...)...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	object, err := jsonObject(result.Stdout)
	if err != nil {
		return err
	}
	got, err := labelsField(object, "labels")
	if err != nil {
		return err
	}
	if err := compareLabels(got, expectedLabels(s, p), s.fps); err != nil {
		return err
	}
	calls, err := tr.lines()
	if err != nil {
		return err
	}
	if want := sampledFrames(s, p); len(calls) != want {
		return fmt.Errorf("expected fwdetect.detect to be called once per sampled frame (%d frames, every %d: %d calls), got %d calls", s.frames, p.every, want, len(calls))
	}
	harness.Logger.Successf("✓ %s (%d detector calls, %.1f s)", formatLabels(got), len(calls), elapsed.Seconds())
	if maxRSS > 0 {
		if peak > maxRSS {
			return fmt.Errorf("expected your program to stay under %.0f MiB of memory (all its processes together), it peaked at %.0f MiB. Read frames one at a time instead of keeping them", mib(maxRSS), mib(peak))
		}
		harness.Logger.Successf("✓ peak memory %.0f MiB (limit %.0f MiB)", mib(peak), mib(maxRSS))
	}
	return nil
}

func testAnalyze(harness *test_case_harness.TestCaseHarness) error {
	if err := analyzeScene(harness, sceneStreet, defaultProfile, 0); err != nil {
		return err
	}
	if err := analyzeScene(harness, sceneStreet, profile{5, 0.5, allLabels}, 0, "--every", "5"); err != nil {
		return err
	}
	return analyzeScene(harness, sceneNoisyStreet, profile{2, 0.3, allLabels}, 0, "--every", "2", "--max-gap", "0.3")
}

// an2's budget: well above Python, NumPy and OpenCV with one frame at a time,
// well below the ~1 GB the long clip's decoded frames take.
const analyzeLongMaxRSS = 400 << 20

func testAnalyzeLong(harness *test_case_harness.TestCaseHarness) error {
	return analyzeScene(harness, sceneLong, profile{10, 2, allLabels}, analyzeLongMaxRSS, "--every", "10", "--max-gap", "2")
}

const testProfilesYAML = `# Written by the tester.
profiles:
  default:
    every: 1
    max_gap_s: 0.5
    labels: [person, car, bicycle]
  people:
    every: 2
    max_gap_s: 0.5
    labels: [person]
  traffic:
    every: 5
    max_gap_s: 1.0
    labels: [person, car]
`

var testProfiles = map[string]profile{
	"default": defaultProfile,
	"people":  {2, 0.5, []string{"person"}},
	"traffic": {5, 1.0, []string{"person", "car"}},
}

func writeProfiles(harness *test_case_harness.TestCaseHarness, content string) string {
	return writeProfilesNamed(harness, "profiles", content)
}

func writeProfilesNamed(harness *test_case_harness.TestCaseHarness, kind, content string) string {
	path := filepath.Join(tempDir(harness), fmt.Sprintf("%s-%s.yaml", kind, random.RandomWord()))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
	return path
}

func testProfilesYAMLStage(harness *test_case_harness.TestCaseHarness) error {
	config := writeProfiles(harness, testProfilesYAML)
	harness.Logger.Infof("Profiles in %s:\n%s", filepath.Base(config), testProfilesYAML)
	names := []string{"people", "traffic"}
	sort.Strings(names)
	for _, name := range names {
		if err := analyzeScene(harness, sceneStreet, testProfiles[name], 0, "--config", config, "--profile", name); err != nil {
			return fmt.Errorf("profile %s: %v", name, err)
		}
	}

	street := sceneStreet.write(tempDir(harness))
	unknown := random.RandomWord()
	bad := []struct {
		what, yaml, profile, mention string
	}{
		{"an unknown profile", testProfilesYAML, unknown, unknown},
		{"every: 0", strings.Replace(testProfilesYAML, "every: 2", "every: 0", 1), "people", "profiles.people.every"},
		{"an unknown label", strings.Replace(testProfilesYAML, "[person, car]", "[person, dragon]", 1), "traffic", "profiles.traffic.labels"},
		{"a negative max_gap_s", strings.Replace(testProfilesYAML, "max_gap_s: 1.0", "max_gap_s: -1", 1), "traffic", "profiles.traffic.max_gap_s"},
		{"a misspelled setting", strings.Replace(testProfilesYAML, "max_gap_s: 0.5\n    labels: [person]", "max_gap: 0.5\n    labels: [person]", 1), "people", "profiles.people.max_gap"},
		{"a file that isn't YAML", "profiles: [unclosed\n", "people", ""},
	}
	for _, b := range bad {
		config := writeProfiles(harness, b.yaml)
		mention := b.mention
		if mention == "" {
			mention = filepath.Base(config)
		}
		harness.Logger.Infof("With %s:", b.what)
		result, err := runCLI(harness, "analyze", street, "--config", config, "--profile", b.profile)
		if err != nil {
			return err
		}
		if err := expectCleanFailure(result, mention); err != nil {
			return fmt.Errorf("with %s: %v", b.what, err)
		}
		harness.Logger.Successf("✓ exit code %d, stderr names %s", result.ExitCode, mention)
	}
	missing := filepath.Join(tempDir(harness), random.RandomWord()+".yaml")
	harness.Logger.Infof("With a config file that doesn't exist:")
	result, err := runCLI(harness, "analyze", street, "--config", missing, "--profile", "people")
	if err != nil {
		return err
	}
	if err := expectCleanFailure(result, filepath.Base(missing)); err != nil {
		return fmt.Errorf("with a missing config file: %v", err)
	}
	harness.Logger.Successf("✓ exit code %d, stderr names the file", result.ExitCode)
	return nil
}
