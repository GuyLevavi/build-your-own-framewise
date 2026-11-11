package internal

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/codecrafters-io/tester-utils/executable"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// proc is a long-running `./your_program.sh ...` (a server or a worker) in a
// process group of its own, so signals reach everything it started.
type proc struct {
	harness *test_case_harness.TestCaseHarness
	name    string
	exe     *executable.Executable
	pid     int
	done    chan struct{}
	result  executable.ExecutableResult
	err     error
}

// startProc starts the program with env added to the tester's environment
// (and the variables in `clear` removed).
func startProc(harness *test_case_harness.TestCaseHarness, name string, env map[string]string, args ...string) (*proc, error) {
	for _, k := range []string{"FRAMEWISE_API_KEY", "FRAMEWISE_MAX_UPLOAD_MB", "FRAMEWISE_PROFILES", "FRAMEWISE_EVENTS",
		"FRAMEWISE_LEASE_S", "FRAMEWISE_MAX_ATTEMPTS", "FRAMEWISE_DETECT_PROCESSES", "DATABASE_URL", "FWDETECT_TRACE",
		"FRAMEWISE_MODEL_URL", "FRAMEWISE_MODEL_TIMEOUT_S"} {
		os.Unsetenv(k)
	}
	var shown []string
	for k, v := range env {
		if strings.Contains(k, "URL") || strings.Contains(k, "KEY") || strings.Contains(v, "/") {
			shown = append(shown, k+"=...")
		} else {
			shown = append(shown, k+"="+v)
		}
	}
	sort.Strings(shown)
	harness.Logger.Infof("$ %s./your_program.sh %s", strings.Join(append(shown, ""), " "), strings.Join(args, " "))
	exe := harness.NewExecutable()
	exe.TimeoutInMilliseconds = 30 * 60 * 1000 // stopped by the test, or at teardown
	exe.MemoryLimitInBytes = 0                 // the stages that care measure memory themselves
	var err error
	withEnv(env, func() { err = exe.Start(args...) })
	if err != nil {
		return nil, fmt.Errorf("couldn't start your program: %v", err)
	}
	p := &proc{harness: harness, name: name, exe: exe, pid: exe.Process.Pid, done: make(chan struct{})}
	go func() {
		p.result, p.err = exe.Wait()
		close(p.done)
	}()
	harness.RegisterTeardownFunc(func() {
		p.signal(syscall.SIGKILL)
		p.wait(5 * time.Second)
	})
	return p, nil
}

func (p *proc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *proc) signal(sig syscall.Signal) {
	if !p.exited() {
		syscall.Kill(-p.pid, sig)
	}
}

// wait reports whether the program exited within timeout.
func (p *proc) wait(timeout time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// stop sends SIGTERM and waits; after 10 s it sends SIGKILL.
func (p *proc) stop() {
	p.harness.Logger.Infof("Stopping the %s (SIGTERM)", p.name)
	p.signal(syscall.SIGTERM)
	if !p.wait(10 * time.Second) {
		p.signal(syscall.SIGKILL)
		p.wait(5 * time.Second)
	}
}

// kill sends SIGKILL, as a crash or an OOM kill would.
func (p *proc) kill() {
	p.harness.Logger.Infof("Killing the %s (SIGKILL)", p.name)
	p.signal(syscall.SIGKILL)
	p.wait(10 * time.Second)
}

// stderr is what the program wrote to stderr, once it has exited.
func (p *proc) stderr() string { return string(p.result.Stderr) }
