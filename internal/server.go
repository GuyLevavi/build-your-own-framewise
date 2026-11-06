package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/executable"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// server is a running `./your_program.sh serve`.
type server struct {
	harness *test_case_harness.TestCaseHarness
	exe     *executable.Executable
	base    string
	apiKey  string // sent as X-API-Key when set
	client  *http.Client
}

func freePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: no free port: %v", err))
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// startServer starts the server with env set (and every other FRAMEWISE_*
// variable unset), and waits up to 30 s for GET /health to answer 200.
func startServer(harness *test_case_harness.TestCaseHarness, dataDir string, env map[string]string) (*server, error) {
	for _, name := range []string{"FRAMEWISE_API_KEY", "FRAMEWISE_MAX_UPLOAD_MB"} {
		os.Unsetenv(name)
	}
	var shown []string
	for name, value := range env {
		os.Setenv(name, value)
		shown = append(shown, name+"="+value)
	}
	defer func() {
		for name := range env {
			os.Unsetenv(name)
		}
	}()

	port := freePort()
	args := []string{"serve", "--port", fmt.Sprint(port), "--data-dir", dataDir}
	harness.Logger.Infof("$ %s./your_program.sh %s", strings.Join(append(shown, ""), " "), strings.Join(args, " "))
	exe := harness.NewExecutable()
	exe.TimeoutInMilliseconds = 10 * 60 * 1000 // stopped by the test, or at teardown
	if err := exe.Start(args...); err != nil {
		return nil, fmt.Errorf("couldn't start your program: %v", err)
	}
	s := &server{
		harness: harness,
		exe:     exe,
		base:    fmt.Sprintf("http://127.0.0.1:%d", port),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	harness.RegisterTeardownFunc(func() { exe.Kill() })

	deadline := time.Now().Add(30 * time.Second)
	for {
		if exe.HasExited() {
			return nil, fmt.Errorf("your program exited before answering GET /health")
		}
		response, err := (&http.Client{Timeout: time.Second}).Get(s.base + "/health")
		if err == nil {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode == 200 {
				harness.Logger.Successf("✓ GET /health answered 200 on port %d", port)
				return s, nil
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("expected GET /health to answer 200, got %d %s", response.StatusCode, truncate(string(body)))
			}
		} else if time.Now().After(deadline) {
			return nil, fmt.Errorf("expected a server on 127.0.0.1:%d within 30 seconds, but couldn't connect: %v", port, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stop sends SIGTERM to the server's process group.
func (s *server) stop() {
	s.harness.Logger.Infof("Stopping the server (SIGTERM)")
	s.exe.Kill()
}

type response struct {
	status int
	raw    []byte
	object map[string]any // nil when the body isn't a JSON object
	took   time.Duration
}

func (r response) describe() string { return fmt.Sprintf("%d %s", r.status, truncate(string(r.raw))) }

func (s *server) send(method, path, contentType string, body []byte, headers map[string]string) (response, error) {
	request, err := http.NewRequest(method, s.base+path, bytes.NewReader(body))
	if err != nil {
		return response{}, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if s.apiKey != "" {
		request.Header.Set("X-API-Key", s.apiKey)
	}
	for k, v := range headers {
		if v == "" {
			request.Header.Del(k)
		} else {
			request.Header.Set(k, v)
		}
	}
	start := time.Now()
	resp, err := s.client.Do(request)
	if err != nil {
		if s.exe.HasExited() {
			return response{}, fmt.Errorf("%s %s: your server has exited", method, path)
		}
		return response{}, fmt.Errorf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	r := response{status: resp.StatusCode, raw: raw, took: time.Since(start)}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if decoder.Decode(&object) == nil {
		r.object = object
	}
	return r, nil
}

func (s *server) get(path string, headers map[string]string) (response, error) {
	s.harness.Logger.Infof("GET %s", path)
	return s.send("GET", path, "", nil, headers)
}

func (s *server) postJSON(path string, body any) (response, error) {
	data, _ := json.Marshal(body)
	s.harness.Logger.Infof("POST %s %s", path, data)
	return s.send("POST", path, "application/json", data, nil)
}

func (s *server) upload(filename string, data []byte, headers map[string]string) (response, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", filename)
	part.Write(data)
	writer.Close()
	s.harness.Logger.Infof("POST /videos (file=%q, %d bytes)", filename, len(data))
	return s.send("POST", "/videos", writer.FormDataContentType(), body.Bytes(), headers)
}

// expectStatus checks the status code, and for errors that the body is JSON with a "detail" field.
func expectStatus(r response, want int) error {
	if r.status != want {
		return fmt.Errorf("expected status %d, got %s", want, r.describe())
	}
	if r.object == nil {
		return fmt.Errorf("expected a JSON object body, got %q", truncate(string(r.raw)))
	}
	if want >= 400 {
		if _, ok := r.object["detail"]; !ok {
			return fmt.Errorf("expected the error body to have a \"detail\" field, got %s", truncate(string(r.raw)))
		}
	}
	return nil
}

func stringField(object map[string]any, key string) (string, error) {
	value, ok := object[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("expected %q to be a non-empty string, got %v", key, object[key])
	}
	return value, nil
}

// uploadClip uploads data as filename and returns the video id.
func (s *server) uploadClip(filename string, data []byte) (string, error) {
	r, err := s.upload(filename, data, nil)
	if err != nil {
		return "", err
	}
	if err := expectStatus(r, 201); err != nil {
		return "", err
	}
	id, err := stringField(r.object, "video_id")
	if err != nil {
		return "", err
	}
	s.harness.Logger.Successf("✓ 201, video_id %s", id)
	return id, nil
}

// submitJob posts a job and returns its id and the status in the answer.
func (s *server) submitJob(videoID, detector string) (string, string, error) {
	body := map[string]string{"video_id": videoID}
	if detector != "" {
		body["detector"] = detector
	}
	r, err := s.postJSON("/jobs", body)
	if err != nil {
		return "", "", err
	}
	if err := expectStatus(r, 202); err != nil {
		return "", "", err
	}
	id, err := stringField(r.object, "id")
	if err != nil {
		return "", "", err
	}
	status, _ := r.object["status"].(string)
	return id, status, nil
}

// waitJob polls GET /jobs/{id} until DONE or FAILED.
func (s *server) waitJob(id string, timeout time.Duration) (map[string]any, error) {
	s.harness.Logger.Infof("Polling GET /jobs/%s until DONE or FAILED", id)
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
		switch status {
		case "DONE", "FAILED":
			return r.object, nil
		case "PENDING", "RUNNING":
		default:
			return nil, fmt.Errorf("expected status PENDING, RUNNING, DONE or FAILED, got %v", r.object["status"])
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("job %s still %s after %s", id, status, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// expectJobDone waits for the job and compares its segments with c's.
func (s *server) expectJobDone(id string, c clip) (map[string]any, error) {
	job, err := s.waitJob(id, 60*time.Second)
	if err != nil {
		return nil, err
	}
	if job["status"] != "DONE" {
		return nil, fmt.Errorf("expected job %s to be DONE, got %v (error: %v)", id, job["status"], job["error"])
	}
	got, err := segmentsField(job, "segments")
	if err != nil {
		return nil, err
	}
	if err := compareSegments(got, c.segments(defaultMaxGapS), c.fps); err != nil {
		return nil, err
	}
	s.harness.Logger.Successf("✓ DONE: %s", formatSegments(got))
	return job, nil
}

// runJob uploads c, runs a job on it and checks the result.
func (s *server) runJob(c clip) error {
	videoID, err := s.uploadClip(fmt.Sprintf("%s.avi", c.name), c.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := s.submitJob(videoID, "")
	if err != nil {
		return err
	}
	_, err = s.expectJobDone(jobID, c)
	return err
}
