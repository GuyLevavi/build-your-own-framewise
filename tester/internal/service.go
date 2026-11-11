package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// service is a server plus its workers (`./your_program.sh work`, from stage wk1).
type service struct {
	harness *test_case_harness.TestCaseHarness
	*server
	dataDir string
	env     map[string]string
	mu      sync.Mutex
	workers []*proc
	stopSup chan struct{}
}

// serviceEnv is the environment every server and worker of a stage gets.
func serviceEnv(harness *test_case_harness.TestCaseHarness, dbURL string, extra map[string]string) map[string]string {
	env := map[string]string{"FRAMEWISE_PROFILES": writeProfiles(harness, testProfilesYAML)}
	if dbURL != "" {
		env["DATABASE_URL"] = dbURL
	}
	for k, v := range extra {
		env[k] = v
	}
	return env
}

// startService starts the server, then n workers. With lenient set (stages
// before wk1) a worker that exits at once just means there's no `work` command yet.
func startService(harness *test_case_harness.TestCaseHarness, dataDir string, env map[string]string, n int, lenient bool) (*service, error) {
	s, err := startServer(harness, dataDir, env)
	if err != nil {
		return nil, err
	}
	sv := &service{harness: harness, server: s, dataDir: dataDir, env: env}
	for i := 0; i < n; i++ {
		w, err := sv.addWorker()
		if err != nil {
			return nil, err
		}
		if lenient {
			if w.wait(1500 * time.Millisecond) {
				harness.Logger.Infof("(no `work` command yet: fine before stage wk1)")
				sv.mu.Lock()
				sv.workers = nil
				sv.mu.Unlock()
				break
			}
		}
	}
	return sv, nil
}

func (sv *service) addWorker() (*proc, error) {
	sv.mu.Lock()
	name := fmt.Sprintf("worker %d", len(sv.workers)+1)
	sv.mu.Unlock()
	w, err := startProc(sv.harness, name, sv.env, "work")
	if err != nil {
		return nil, err
	}
	sv.mu.Lock()
	sv.workers = append(sv.workers, w)
	sv.mu.Unlock()
	return w, nil
}

// supervise restarts any worker that exits, as Kubernetes restarts a crashed pod.
func (sv *service) supervise() {
	sv.stopSup = make(chan struct{})
	sv.harness.RegisterTeardownFunc(func() { sv.stopSupervising() })
	go func() {
		for {
			select {
			case <-sv.stopSup:
				return
			case <-time.After(300 * time.Millisecond):
			}
			sv.mu.Lock()
			var dead []int
			for i, w := range sv.workers {
				if w != nil && w.exited() {
					dead = append(dead, i)
				}
			}
			sv.mu.Unlock()
			for _, i := range dead {
				sv.harness.Logger.Infof("worker %d exited (code %d); starting it again", i+1, sv.workers[i].result.ExitCode)
				w, err := startProc(sv.harness, fmt.Sprintf("worker %d", i+1), sv.env, "work")
				if err != nil {
					continue
				}
				sv.mu.Lock()
				sv.workers[i] = w
				sv.mu.Unlock()
			}
		}
	}()
}

func (sv *service) stopSupervising() {
	if sv.stopSup != nil {
		select {
		case <-sv.stopSup:
		default:
			close(sv.stopSup)
		}
	}
}

// workerOf finds the worker whose process group holds pid.
func (sv *service) workerOf(pid int) *proc {
	pgid := pgidOf(pid)
	sv.mu.Lock()
	defer sv.mu.Unlock()
	for _, w := range sv.workers {
		if w != nil && w.pid == pgid {
			return w
		}
	}
	return nil
}

func pgidOf(pid int) int {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return -1
	}
	s := string(stat)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(fields) < 3 {
		return -1
	}
	pgid, _ := strconv.Atoi(fields[2])
	return pgid
}

// waitStatus polls the job until its status is one of want.
func (s *server) waitStatus(id string, timeout time.Duration, want ...string) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	for {
		r, err := s.send("GET", "/jobs/"+id, "", nil, nil)
		if err != nil {
			return nil, err
		}
		if err := expectStatus(r, 200); err != nil {
			return nil, fmt.Errorf("GET /jobs/%s: %v", id, err)
		}
		status, _ := r.object["status"].(string)
		for _, w := range want {
			if status == w {
				return r.object, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("expected job %s to become %s within %s, it is still %s", id, strings.Join(want, " or "), timeout, status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
