package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// server is a running `./your_program.sh serve`.
type server struct {
	harness *test_case_harness.TestCaseHarness
	p       *proc
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
	port := freePort()
	p, err := startProc(harness, "server", env, "serve", "--port", fmt.Sprint(port), "--data-dir", dataDir)
	if err != nil {
		return nil, err
	}
	s := &server{
		harness: harness,
		p:       p,
		base:    fmt.Sprintf("http://127.0.0.1:%d", port),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if p.exited() {
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
func (s *server) stop() { s.p.stop() }

type response struct {
	status int
	raw    []byte
	object map[string]any // nil when the body isn't a JSON object
	took   time.Duration
}

func (r response) describe() string { return fmt.Sprintf("%d %s", r.status, truncate(string(r.raw))) }

func (s *server) send(method, path, contentType string, body []byte, headers map[string]string, stream ...io.Reader) (response, error) {
	var reader io.Reader = bytes.NewReader(body)
	if len(stream) > 0 {
		reader = stream[0]
	}
	request, err := http.NewRequest(method, s.base+path, reader)
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
		if s.p.exited() {
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
func (s *server) submitJob(videoID, profileName string) (string, string, error) {
	body := map[string]string{"video_id": videoID}
	if profileName != "" {
		body["profile"] = profileName
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

// expectJobDone waits for the job and compares its labels with what p finds in sc.
func (s *server) expectJobDone(id string, sc scene, p profile) (map[string]any, error) {
	job, err := s.waitJob(id, 120*time.Second)
	if err != nil {
		return nil, err
	}
	if job["status"] != "DONE" {
		return nil, fmt.Errorf("expected job %s to be DONE, got %v (error: %v)", id, job["status"], job["error"])
	}
	got, err := labelsField(job, "labels")
	if err != nil {
		return nil, err
	}
	if err := compareLabels(got, expectedLabels(sc, p), sc.fps); err != nil {
		return nil, err
	}
	s.harness.Logger.Successf("✓ DONE: %s", formatLabels(got))
	return job, nil
}

// runJob uploads sc, runs a job on it with the default profile and checks the result.
func (s *server) runJob(sc scene) error {
	videoID, err := s.uploadClip(fmt.Sprintf("%s.avi", sc.name), sc.bytes())
	if err != nil {
		return err
	}
	jobID, _, err := s.submitJob(videoID, "")
	if err != nil {
		return err
	}
	_, err = s.expectJobDone(jobID, sc, defaultProfile)
	return err
}

// uploadStream posts a multipart upload of size bytes without holding it in
// memory: the body is written through a pipe as the server reads it.
func (s *server) uploadStream(filename string, size int64) (response, error) {
	reader, writer := io.Pipe()
	form := multipart.NewWriter(writer)
	go func() {
		part, err := form.CreateFormFile("file", filename)
		if err == nil {
			chunk := bytes.Repeat([]byte("framewise "), 6554) // 64 KiB
			for left := size; left > 0 && err == nil; {
				n := min(left, int64(len(chunk)))
				_, err = part.Write(chunk[:n])
				left -= n
			}
		}
		if err == nil {
			err = form.Close()
		}
		writer.CloseWithError(err)
	}()
	s.harness.Logger.Infof("POST /videos (file=%q, %.0f MiB, streamed)", filename, mib(size))
	return s.send("POST", "/videos", form.FormDataContentType(), nil, map[string]string{}, reader)
}
