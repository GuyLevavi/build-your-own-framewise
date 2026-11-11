package internal

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// modelServer stands in for the team's Triton: the course's two models,
// yolo26n and yolo26n-cls, behind Triton's HTTP API (the KServe v2 inference
// protocol), with JSON or binary tensors. It serves the learner's own
// models/*.onnx with onnxruntime (hook/model_backend.py, one process per
// instance, run by the learner's Python), at the input size its config gives,
// in batches of at most maxBatch, with instances requests in progress at
// once; the rest wait their turn. Each request also costs latency, the
// network and queueing a real deployment adds.
//
// The tester can make it fail: answer 503 to every failEvery-th request, hang
// (accept requests and never answer), or go away (refuse connections). A
// uniform grey image makes the request fail with 500, as a crashing backend does.
type modelServer struct {
	harness   *test_case_harness.TestCaseHarness
	port      int
	size      int // yolo26n's input is size x size
	maxBatch  int
	instances int
	latency   time.Duration
	perFrame  time.Duration

	slots    chan struct{}
	backends chan *modelBackend
	mu       sync.Mutex
	listener net.Listener
	server   *http.Server
	stats    modelStats
	// failure modes
	failEvery int
	hanging   bool
	hangDone  chan struct{}
}

type modelStats struct {
	requests, ok, busy, failed, rejected int
	frames                               int // frames in requests answered 200
	framesBy                             map[string]int
	batches                              []int
	inFlight, maxInFlight                int
	first, last                          time.Time // first request received, last 200 answered
}

const (
	detectModel   = "yolo26n"
	classifyModel = "yolo26n-cls"
	classifySize  = 224
	maxInstances  = 2
)

// servedModel is what the server publishes about a model.
type servedModel struct {
	name    string
	size    int
	outDims []int // per image
}

func (m *modelServer) model(name string) (servedModel, bool) {
	switch name {
	case detectModel:
		return servedModel{detectModel, m.size, []int{300, 6}}, true
	case classifyModel:
		return servedModel{classifyModel, classifySize, []int{1000}}, true
	}
	return servedModel{}, false
}

func startModelServer(harness *test_case_harness.TestCaseHarness, size int) (*modelServer, error) {
	m := &modelServer{
		harness: harness, size: size,
		maxBatch: 16, instances: maxInstances, latency: 5 * time.Millisecond,
	}
	backends, err := startModelBackends(harness, maxInstances)
	if err != nil {
		return nil, err
	}
	m.backends = backends
	if err := m.listen(freePort()); err != nil {
		return nil, err
	}
	harness.RegisterTeardownFunc(func() { m.goAway() })
	return m, nil
}

func (m *modelServer) url() string { return fmt.Sprintf("http://127.0.0.1:%d", m.port) }

func (m *modelServer) listen(port int) error {
	var listener net.Listener
	var err error
	for range 50 { // the port may take a moment to be free again after goAway
		if listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("CodeCrafters Internal Error: the model server couldn't listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/health/live", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /v2/health/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /v2/models/{model}", m.metadata)
	mux.HandleFunc("GET /v2/models/{model}/ready", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.model(r.PathValue("model")); !ok {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /v2/models/{model}/config", m.config)
	mux.HandleFunc("POST /v2/models/{model}/infer", m.infer)
	mux.HandleFunc("POST /v2/models/{model}/versions/{version}/infer", m.infer)
	m.mu.Lock()
	m.port = port
	m.listener = listener
	m.server = &http.Server{Handler: mux}
	m.slots = make(chan struct{}, min(m.instances, maxInstances))
	m.hangDone = make(chan struct{})
	server := m.server
	m.mu.Unlock()
	go server.Serve(listener)
	return nil
}

// set changes the cost model; call it before the program starts sending.
func (m *modelServer) set(maxBatch, instances int, latency, perFrame time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxBatch, m.instances, m.latency, m.perFrame = maxBatch, instances, latency, perFrame
	m.slots = make(chan struct{}, min(instances, maxInstances))
}

// goAway closes the server: connections are refused until comeBack.
func (m *modelServer) goAway() {
	m.mu.Lock()
	server := m.server
	m.server = nil
	if m.hangDone != nil {
		select {
		case <-m.hangDone:
		default:
			close(m.hangDone)
		}
	}
	m.mu.Unlock()
	if server != nil {
		server.Close()
	}
}

func (m *modelServer) comeBack() error { return m.listen(m.port) }

func (m *modelServer) setFailEvery(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failEvery = n
}

func (m *modelServer) setHanging(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hanging = on
	if !on {
		select {
		case <-m.hangDone:
		default:
			close(m.hangDone)
		}
		m.hangDone = make(chan struct{})
	}
}

func (m *modelServer) resetStats() {
	m.mu.Lock()
	defer m.mu.Unlock()
	inFlight := m.stats.inFlight
	m.stats = modelStats{inFlight: inFlight, framesBy: map[string]int{}}
}

func (m *modelServer) snapshot() modelStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.stats
	s.batches = append([]int(nil), m.stats.batches...)
	return s
}

// rate is frames per second answered, from the first request to the last answer.
func (s modelStats) rate() float64 {
	if s.frames == 0 || !s.last.After(s.first) {
		return 0
	}
	return float64(s.frames) / s.last.Sub(s.first).Seconds()
}

func (s modelStats) meanBatch() float64 {
	if len(s.batches) == 0 {
		return 0
	}
	total := 0
	for _, b := range s.batches {
		total += b
	}
	return float64(total) / float64(len(s.batches))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	data, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}

func modelError(w http.ResponseWriter, status int, format string, args ...any) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf(format, args...)})
}

func modelNotFound(w http.ResponseWriter, name string) {
	modelError(w, 404, "Request for unknown model: '%s' is not found (this server has %s and %s)", name, detectModel, classifyModel)
}

func (m *modelServer) metadata(w http.ResponseWriter, r *http.Request) {
	model, ok := m.model(r.PathValue("model"))
	if !ok {
		modelNotFound(w, r.PathValue("model"))
		return
	}
	writeJSON(w, 200, map[string]any{
		"name":     model.name,
		"versions": []string{"1"},
		"platform": "onnxruntime_onnx",
		"inputs": []map[string]any{
			{"name": "images", "datatype": "FP32", "shape": []int{-1, 3, model.size, model.size}},
		},
		"outputs": []map[string]any{
			{"name": "output0", "datatype": "FP32", "shape": append([]int{-1}, model.outDims...)},
		},
	})
}

func (m *modelServer) config(w http.ResponseWriter, r *http.Request) {
	model, ok := m.model(r.PathValue("model"))
	if !ok {
		modelNotFound(w, r.PathValue("model"))
		return
	}
	m.mu.Lock()
	maxBatch, instances := m.maxBatch, m.instances
	m.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"name":           model.name,
		"platform":       "onnxruntime_onnx",
		"max_batch_size": maxBatch,
		"input": []map[string]any{
			{"name": "images", "data_type": "TYPE_FP32", "dims": []int{3, model.size, model.size}},
		},
		"output": []map[string]any{
			{"name": "output0", "data_type": "TYPE_FP32", "dims": model.outDims},
		},
		"instance_group": []map[string]any{{"count": instances, "kind": "KIND_CPU"}},
	})
}

type inferTensor struct {
	Name       string         `json:"name"`
	Shape      []int          `json:"shape"`
	Datatype   string         `json:"datatype"`
	Parameters map[string]any `json:"parameters"`
	Data       any            `json:"data"`
}

type inferRequest struct {
	ID         string         `json:"id"`
	Inputs     []inferTensor  `json:"inputs"`
	Outputs    []inferTensor  `json:"outputs"`
	Parameters map[string]any `json:"parameters"`
}

func (m *modelServer) infer(w http.ResponseWriter, r *http.Request) {
	model, known := m.model(r.PathValue("model"))
	m.mu.Lock()
	m.stats.requests++
	n := m.stats.requests
	if m.stats.first.IsZero() {
		m.stats.first = time.Now()
	}
	m.stats.inFlight++
	m.stats.maxInFlight = max(m.stats.maxInFlight, m.stats.inFlight)
	failEvery, hanging, hangDone, slots := m.failEvery, m.hanging, m.hangDone, m.slots
	latency, perFrame, maxBatch := m.latency, m.perFrame, m.maxBatch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.stats.inFlight--
		m.mu.Unlock()
	}()

	if hanging {
		select {
		case <-r.Context().Done():
		case <-hangDone:
		}
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	if !known {
		m.count(func(s *modelStats) { s.rejected++ })
		modelNotFound(w, r.PathValue("model"))
		return
	}
	if failEvery > 0 && n%failEvery == 0 {
		m.count(func(s *modelStats) { s.busy++ })
		modelError(w, 503, "Server is busy: the model has too many requests queued, try again later")
		return
	}
	req, data, err := parseInfer(r.Header.Get("Inference-Header-Content-Length"), body)
	if err != nil {
		m.count(func(s *modelStats) { s.rejected++ })
		modelError(w, 400, "%v", err)
		return
	}
	pixels, batch, err := m.images(model, req, data, maxBatch)
	if err != nil {
		m.count(func(s *modelStats) { s.rejected++ })
		modelError(w, 400, "%v", err)
		return
	}
	for i := range batch {
		if isGreyImage(pixels, i, model.size) {
			m.count(func(s *modelStats) { s.failed++ })
			modelError(w, 500, "inference failed: the onnxruntime backend crashed on image %d of the batch (a uniform grey image)", i)
			return
		}
	}

	// Wait for an instance, then run the batch.
	select {
	case slots <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	time.Sleep(latency + time.Duration(batch)*perFrame)
	backend := <-m.backends
	shape, out, err := backend.run(model.name, []int{batch, 3, model.size, model.size}, pixels)
	m.backends <- backend
	<-slots
	if err != nil {
		m.count(func(s *modelStats) { s.failed++ })
		modelError(w, 500, "inference failed: %v", err)
		return
	}
	m.count(func(s *modelStats) {
		s.ok++
		s.frames += batch
		if s.framesBy == nil {
			s.framesBy = map[string]int{}
		}
		s.framesBy[model.name] += batch
		s.batches = append(s.batches, batch)
		s.last = time.Now()
	})

	if wantsBinary(req) {
		header, _ := json.Marshal(map[string]any{
			"model_name": model.name, "model_version": "1", "id": req.ID,
			"outputs": []map[string]any{{"name": "output0", "datatype": "FP32", "shape": shape,
				"parameters": map[string]any{"binary_data_size": len(out)}}},
		})
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Inference-Header-Content-Length", strconv.Itoa(len(header)))
		w.WriteHeader(200)
		w.Write(header)
		w.Write(out)
		return
	}
	values := make([]float32, len(out)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(out[4*i:]))
	}
	writeJSON(w, 200, map[string]any{
		"model_name": model.name, "model_version": "1", "id": req.ID,
		"outputs": []map[string]any{{"name": "output0", "datatype": "FP32", "shape": shape, "data": values}},
	})
}

func (m *modelServer) count(f func(*modelStats)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(&m.stats)
}

// parseInfer splits a request into its JSON header and the binary data after it.
func parseInfer(headerLength string, body []byte) (inferRequest, []byte, error) {
	var req inferRequest
	jsonPart, data := body, []byte(nil)
	if headerLength != "" {
		n, err := strconv.Atoi(headerLength)
		if err != nil || n < 0 || n > len(body) {
			return req, nil, fmt.Errorf("Inference-Header-Content-Length %q doesn't fit a body of %d bytes", headerLength, len(body))
		}
		jsonPart, data = body[:n], body[n:]
	}
	if err := json.Unmarshal(jsonPart, &req); err != nil {
		return req, nil, fmt.Errorf("the request's JSON header isn't valid JSON: %v", err)
	}
	return req, data, nil
}

func wantsBinary(req inferRequest) bool {
	for _, o := range req.Outputs {
		if b, ok := o.Parameters["binary_data"].(bool); ok && b {
			return true
		}
	}
	if b, ok := req.Parameters["binary_data_output"].(bool); ok && b {
		return true
	}
	return false
}

// images checks the input tensor and returns it as little-endian float32 bytes, and its batch size.
func (m *modelServer) images(model servedModel, req inferRequest, data []byte, maxBatch int) ([]byte, int, error) {
	if len(req.Inputs) != 1 || req.Inputs[0].Name != "images" {
		var names []string
		for _, in := range req.Inputs {
			names = append(names, in.Name)
		}
		return nil, 0, fmt.Errorf("expected one input named \"images\", got %v", names)
	}
	in := req.Inputs[0]
	if in.Datatype != "FP32" {
		return nil, 0, fmt.Errorf("input \"images\" must be FP32, got %q", in.Datatype)
	}
	if len(in.Shape) != 4 || in.Shape[1] != 3 || in.Shape[2] != model.size || in.Shape[3] != model.size {
		return nil, 0, fmt.Errorf("input \"images\" must have shape [batch, 3, %d, %d], got %v", model.size, model.size, in.Shape)
	}
	batch := in.Shape[0]
	if batch < 1 || batch > maxBatch {
		return nil, 0, fmt.Errorf("batch size %d is outside this model's [1, %d] (max_batch_size in its config)", batch, maxBatch)
	}
	var raw []byte
	if size64, ok := in.Parameters["binary_data_size"]; ok {
		n, _ := size64.(float64)
		if int(n) != len(data) {
			return nil, 0, fmt.Errorf("binary_data_size %d doesn't match the %d bytes after the header", int(n), len(data))
		}
		raw = data
	} else {
		values, err := flattenFloat32(in.Data)
		if err != nil {
			return nil, 0, err
		}
		raw = make([]byte, 4*len(values))
		for i, v := range values {
			binary.LittleEndian.PutUint32(raw[4*i:], math.Float32bits(v))
		}
	}
	if want := batch * 3 * model.size * model.size * 4; len(raw) != want {
		return nil, 0, fmt.Errorf("input \"images\" of shape %v needs %d bytes of FP32, got %d", in.Shape, want, len(raw))
	}
	return raw, batch, nil
}

func flattenFloat32(v any) ([]float32, error) {
	var out []float32
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				if err := walk(item); err != nil {
					return err
				}
			}
		case float64:
			out = append(out, float32(x))
		case nil:
			return errors.New("input \"images\" has no data: send it as JSON \"data\" or as binary (binary_data_size)")
		default:
			return fmt.Errorf("input \"images\" data must be numbers, got %T", v)
		}
		return nil
	}
	return out, walk(v)
}

// isGreyImage is the dropped-feed test the tester's hook uses, on image i of
// an N x 3 x size x size float32 tensor: most pixels near grey 128, and the rest
// close to it (a letterbox's 114).
func isGreyImage(raw []byte, i, size int) bool {
	plane := size * size
	base := i * 3 * plane
	near, grey, n := 0, 0, 0
	for c := range 3 {
		for y := 0; y < size; y += 8 {
			for x := 0; x < size; x += 8 {
				p := base + c*plane + y*size + x
				v := math.Float32frombits(binary.LittleEndian.Uint32(raw[4*p:]))
				if v > 1.5 {
					v /= 255
				}
				if math.Abs(float64(v)-128.0/255) < 8.0/255 {
					near++
				}
				if v > 100.0/255 && v < 140.0/255 {
					grey++
				}
				n++
			}
		}
	}
	return float64(near) > 0.4*float64(n) && float64(grey) > 0.98*float64(n)
}

// calibrate runs one frame on an idle backend and returns how long the model took.
func (m *modelServer) calibrate() (time.Duration, error) {
	data := make([]byte, 3*m.size*m.size*4)
	backend := <-m.backends
	defer func() { m.backends <- backend }()
	var best time.Duration
	for i := range 4 {
		start := time.Now()
		if _, _, err := backend.run(detectModel, []int{1, 3, m.size, m.size}, data); err != nil {
			return 0, fmt.Errorf("CodeCrafters Internal Error: calibrating the model server: %v", err)
		}
		if d := time.Since(start); i > 0 && (best == 0 || d < best) {
			best = d
		}
	}
	return best, nil
}
