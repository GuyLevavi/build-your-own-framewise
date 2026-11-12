package internal

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

//go:embed hook/model_backend.py
var modelBackendSource []byte

// modelBackend is one instance of the model server: a Python process running
// onnxruntime on the learner's models/*.onnx (hook/model_backend.py).
type modelBackend struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// startModelBackends starts n backends with the learner's Python: the nix dev
// shell's when FRAMEWISE_NIX_PYTHON is set, else uv's environment for their
// repository, as your_program.sh does.
func startModelBackends(harness *test_case_harness.TestCaseHarness, n int) (chan *modelBackend, error) {
	repo := repoDir(harness)
	models := filepath.Join(repo, "models")
	if _, err := os.Stat(filepath.Join(models, detectModel+".onnx")); err != nil {
		return nil, fmt.Errorf("the model server serves your models/%s.onnx, and it isn't there: keep the starter's models/ in your repository", detectModel)
	}
	script := filepath.Join(tempDir(harness), "model_backend.py")
	if err := os.WriteFile(script, modelBackendSource, 0o644); err != nil {
		return nil, err
	}
	threads := max(1, availableCPUs()/n)
	backends := make(chan *modelBackend, n)
	for range n {
		cmd := exec.Command("sh", "-c", `if [ -n "${FRAMEWISE_NIX_PYTHON:-}" ]; then exec python -I "$@"; fi; exec uv run --quiet python -I "$@"`,
			"model-backend", script, models, fmt.Sprint(threads))
		cmd.Dir = repo
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("CodeCrafters Internal Error: starting the model server's backend: %v", err)
		}
		b := &modelBackend{cmd: cmd, in: in, out: bufio.NewReaderSize(stdout, 1<<20)}
		harness.RegisterTeardownFunc(func() {
			in.Close()
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			cmd.Wait()
		})
		ready := make(chan error, 1)
		go func() {
			line, err := b.out.ReadBytes('\n')
			if err == nil && string(line) != "{\"ready\": true}\n" {
				err = fmt.Errorf("unexpected %q", line)
			}
			ready <- err
		}()
		select {
		case err := <-ready:
			if err != nil {
				return nil, fmt.Errorf("CodeCrafters Internal Error: the model server's backend didn't start (it runs onnxruntime with your project's Python): %v", err)
			}
		case <-time.After(3 * time.Minute):
			return nil, fmt.Errorf("CodeCrafters Internal Error: the model server's backend didn't start within 3 minutes")
		}
		backends <- b
	}
	return backends, nil
}

// run sends one batch and returns the output's shape and bytes.
func (b *modelBackend) run(model string, shape []int, data []byte) ([]int, []byte, error) {
	header, _ := json.Marshal(map[string]any{"model": model, "shape": shape})
	if _, err := b.in.Write(append(header, '\n')); err != nil {
		return nil, nil, err
	}
	if _, err := b.in.Write(data); err != nil {
		return nil, nil, err
	}
	line, err := b.out.ReadBytes('\n')
	if err != nil {
		return nil, nil, err
	}
	var answer struct {
		Shape []int  `json:"shape"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(line, &answer); err != nil {
		return nil, nil, err
	}
	if answer.Error != "" {
		return nil, nil, fmt.Errorf("%s", answer.Error)
	}
	n := 4
	for _, d := range answer.Shape {
		n *= d
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(b.out, out); err != nil {
		return nil, nil, err
	}
	return answer.Shape, out, nil
}
