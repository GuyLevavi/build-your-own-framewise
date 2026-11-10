package internal

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// The delivery stages (ci1 onwards) drive podman, helm and git on this machine, as a
// learner does on the workstation; tools/run_tests.sh runs them outside the program's container.

// Images the tester uses when nothing else is set. Both ship in the airgap tarball.
const (
	defaultCIImage   = "ghcr.io/astral-sh/uv:python3.12-bookworm-slim"
	defaultBaseImage = defaultCIImage
)

func ciImage() string       { return envOr("FRAMEWISE_CI_IMAGE", defaultCIImage) }
func baseImage() string     { return envOr("FRAMEWISE_BASE_IMAGE", defaultBaseImage) }
func postgresImage() string { return envOr("FRAMEWISE_POSTGRES_IMAGE", defaultPostgresImage) }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// needTool fails the stage with a readable reason when a command isn't installed.
func needTool(name, why string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("this stage needs %s on this machine (%s), and it isn't on PATH", name, why)
	}
	return path, nil
}

// repoDir is the learner's repository.
func repoDir(harness *test_case_harness.TestCaseHarness) string {
	if dir := os.Getenv("CODECRAFTERS_REPOSITORY_DIR"); dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			return abs
		}
	}
	return filepath.Dir(harness.Executable.Path)
}

// command runs name args... and returns its combined output. A timeout of 0 means 10 minutes.
func command(dir string, env []string, timeout time.Duration, name string, args ...string) (string, error) {
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("%s %s didn't finish within %s", name, strings.Join(args, " "), timeout)
	}
	return string(out), err
}

func podman(args ...string) (string, error) {
	return command("", nil, 0, "podman", args...)
}

// tail keeps the last n lines of a long output, for error messages.
func tail(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = append([]string{fmt.Sprintf("... (%d lines before these)", len(lines)-n)}, lines[len(lines)-n:]...)
	}
	return strings.Join(lines, "\n")
}

// indent prefixes every line, so a command's output reads as a block in the log.
func indent(text string) string {
	return "    " + strings.ReplaceAll(strings.TrimRight(text, "\n"), "\n", "\n    ")
}

// copyRepo copies the files git would commit (tracked plus untracked, minus ignored),
// the way a clone in CI or an image build context sees the repository. Without git
// metadata it copies everything but .venv.
func copyRepo(src, dst string) error {
	var files []string
	if _, err := os.Stat(filepath.Join(src, ".git")); err == nil {
		out, err := command(src, nil, time.Minute, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if err != nil {
			return fmt.Errorf("git ls-files in your repository failed: %v\n%s", err, out)
		}
		for _, f := range strings.Split(out, "\x00") {
			if f != "" {
				files = append(files, f)
			}
		}
	} else {
		err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			if d.IsDir() && (d.Name() == ".venv" || d.Name() == "__pycache__") {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, rel := range files {
		from := filepath.Join(src, rel)
		info, err := os.Lstat(from)
		if err != nil {
			continue // deleted but still in the index
		}
		to := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(from)
			if err := os.Symlink(target, to); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: %v", err))
	}
}

// ---- images

// containerfile finds the Containerfile (or Dockerfile) at the repository root.
func containerfile(dir string) (string, error) {
	for _, name := range []string{"Containerfile", "Dockerfile"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("expected a Containerfile (or Dockerfile) at the root of your repository, found neither")
}

// buildImage builds the image from context dir as tag, passing BASE_IMAGE.
func buildImage(harness *test_case_harness.TestCaseHarness, dir, file, tag string) error {
	harness.Logger.Infof("$ podman build -f %s --build-arg BASE_IMAGE=%s -t %s .", file, baseImage(), tag)
	start := time.Now()
	out, err := command(dir, nil, 15*time.Minute, "podman", "build", "-f", file,
		"--build-arg", "BASE_IMAGE="+baseImage(), "-t", tag, ".")
	if err != nil {
		return fmt.Errorf("the image didn't build: %v\n%s", err, tail(out, 30))
	}
	harness.Logger.Successf("✓ built %s in %.0f s", tag, time.Since(start).Seconds())
	return nil
}

type imageInfo struct {
	Size   int64
	Config struct {
		User       string
		WorkingDir string
		Entrypoint []string
		Cmd        []string
	}
}

func inspectImage(tag string) (imageInfo, error) {
	out, err := podman("image", "inspect", tag)
	if err != nil {
		return imageInfo{}, fmt.Errorf("podman image inspect %s: %v\n%s", tag, err, out)
	}
	var infos []imageInfo
	if err := json.Unmarshal([]byte(out), &infos); err != nil || len(infos) == 0 {
		return imageInfo{}, fmt.Errorf("CodeCrafters Internal Error: podman image inspect gave %q", truncate(out))
	}
	return infos[0], nil
}

// imageFile is one entry of an image's filesystem.
type imageFile struct {
	name string // without a leading slash
	mode int64
	gid  int
	dir  bool
	body []byte // only for small regular files
}

// imageFiles lists the image's filesystem, through podman create + export.
func imageFiles(tag string) ([]imageFile, error) {
	out, err := podman("create", tag)
	if err != nil {
		return nil, fmt.Errorf("podman create %s: %v\n%s", tag, err, out)
	}
	id := strings.TrimSpace(out)
	defer podman("rm", "-f", id)
	cmd := exec.Command("podman", "export", id)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var files []imageFile
	reader := tar.NewReader(stdout)
	for {
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			cmd.Wait()
			return nil, fmt.Errorf("reading podman export: %v", err)
		}
		f := imageFile{name: strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/"), mode: h.Mode, gid: h.Gid, dir: h.Typeflag == tar.TypeDir}
		if h.Typeflag == tar.TypeReg && h.Size < 64*1024 {
			f.body, _ = io.ReadAll(reader)
		}
		files = append(files, f)
	}
	return files, cmd.Wait()
}

// ---- containers

// container is a detached `podman run`, standing in for a process of the program.
type container struct {
	harness *test_case_harness.TestCaseHarness
	name    string
}

// runContainer starts image with args, detached, and removes it at teardown.
func runContainer(harness *test_case_harness.TestCaseHarness, label string, options []string, image string, args ...string) (*container, error) {
	name := fmt.Sprintf("framewise-tester-%s-%d", label, random.RandomInt(10000, 99999))
	all := append([]string{"run", "-d", "--name", name}, options...)
	all = append(all, image)
	all = append(all, args...)
	harness.Logger.Infof("$ podman run %s %s %s", strings.Join(shownOptions(options), " "), image, strings.Join(args, " "))
	out, err := podman(all...)
	if err != nil {
		return nil, fmt.Errorf("podman run failed: %v\n%s", err, tail(out, 20))
	}
	c := &container{harness: harness, name: name}
	harness.RegisterTeardownFunc(func() { podman("rm", "-f", "-t", "0", name) })
	return c, nil
}

// shownOptions hides values that look like URLs with credentials in the log.
func shownOptions(options []string) []string {
	shown := make([]string, len(options))
	for i, o := range options {
		if k, v, ok := strings.Cut(o, "="); ok && strings.Contains(v, "://") {
			shown[i] = k + "=..."
		} else {
			shown[i] = o
		}
	}
	return shown
}

func (c *container) state() (running bool, exitCode int, restarts int) {
	out, err := podman("inspect", "--format", "{{.State.Running}} {{.State.ExitCode}} {{.RestartCount}}", c.name)
	if err != nil {
		return false, -1, 0
	}
	fmt.Sscanf(strings.TrimSpace(out), "%t %d %d", &running, &exitCode, &restarts)
	return running, exitCode, restarts
}

func (c *container) exited() bool {
	running, _, _ := c.state()
	return !running
}

func (c *container) stop() {
	c.harness.Logger.Infof("$ podman stop %s", c.name)
	podman("stop", "-t", "10", c.name)
}

func (c *container) kill() {
	c.harness.Logger.Infof("$ podman kill %s", c.name)
	podman("kill", c.name)
}

// crash kills the container's main process from outside, as an OOM kill or a crash would.
// (`podman kill` counts as a deliberate stop, so the restart policy ignores it.)
func (c *container) crash() error {
	out, err := podman("inspect", "--format", "{{.State.Pid}}", c.name)
	if err != nil {
		return fmt.Errorf("podman inspect %s: %v", c.name, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || pid <= 0 {
		return fmt.Errorf("container %s isn't running", c.name)
	}
	c.harness.Logger.Infof("Killing the main process of %s (SIGKILL), as a crash would", c.name)
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		// Rootless podman: the process belongs to a user in podman's user namespace.
		if out, err := podman("unshare", "kill", "-9", fmt.Sprint(pid)); err != nil {
			return fmt.Errorf("couldn't kill process %d of %s: %v %s", pid, c.name, err, out)
		}
	}
	return nil
}

func (c *container) logs() string {
	out, _ := podman("logs", c.name)
	return out
}

// waitExit waits for the container to exit and returns its exit code.
func (c *container) waitExit(timeout time.Duration) (int, error) {
	out, err := command("", nil, timeout, "podman", "wait", c.name)
	if err != nil {
		return -1, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	var code int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &code)
	return code, nil
}

// waitHealthy polls base/health until it answers 200; failed explains why it can't any more.
func waitHealthy(harness *test_case_harness.TestCaseHarness, s *server, timeout time.Duration, hint string) error {
	deadline := time.Now().Add(timeout)
	for {
		if s.p != nil && s.p.exited() {
			return fmt.Errorf("the server's container exited before answering GET /health")
		}
		r, err := s.send("GET", "/health", "", nil, nil)
		if err == nil && r.status == 200 {
			harness.Logger.Successf("✓ GET /health answered 200 at %s", s.base)
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("expected GET %s/health to answer 200 within %s, couldn't connect: %v. %s", s.base, timeout, err, hint)
			}
			return fmt.Errorf("expected GET %s/health to answer 200 within %s, got %s", s.base, timeout, r.describe())
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// containerServer is a server for a container (or a pod) listening at base.
func containerServer(harness *test_case_harness.TestCaseHarness, p running, base string) *server {
	return &server{harness: harness, p: p, base: base, client: &http.Client{Timeout: 30 * time.Second}}
}

// ---- git

// gitEnv is the environment for git commands the tester runs itself.
func gitEnv(extra ...string) []string {
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=Framewise Tester", "GIT_AUTHOR_EMAIL=tester@framewise.invalid",
		"GIT_COMMITTER_NAME=Framewise Tester", "GIT_COMMITTER_EMAIL=tester@framewise.invalid",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	return append(env, extra...)
}

func git(dir string, args ...string) (string, error) {
	out, err := command(dir, gitEnv(), 2*time.Minute, "git", args...)
	if err != nil {
		return out, fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// lines splits output into non-empty lines.
func lines(text string) []string {
	var result []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			result = append(result, l)
		}
	}
	return result
}

// containsAny reports whether text contains any of the words.
func containsAny(text string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}
