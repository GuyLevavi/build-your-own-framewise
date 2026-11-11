package internal

import (
	"bytes"
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

// modelServer stands in for the team's Triton: a model called "fwdetect"
// behind Triton's HTTP API (the KServe v2 inference protocol), with JSON or
// binary tensors. It finds the same coloured boxes fwdetect does, but it
// takes RGB (as models usually do, and OpenCV doesn't), only at its own input
// size, in batches of at most maxBatch frames, with instances requests in
// progress at once; the rest wait their turn. Each request costs latency plus
// perFrame for each frame in it, the shape of a GPU model's cost.
//
// The tester can make it fail: answer 503 to every failEvery-th request, hang
// (accept requests and never answer), or go away (refuse connections). A
// uniform grey frame makes the request fail with 500, as a crashing backend does.
type modelServer struct {
	harness       *test_case_harness.TestCaseHarness
	port          int
	width, height int
	maxBatch      int
	instances     int
	latency       time.Duration
	perFrame      time.Duration

	slots    chan struct{}
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
	batches                              []int
	inFlight, maxInFlight                int
	first, last                          time.Time // first request received, last 200 answered
}

const modelName = "fwdetect"

// modelLabels are the model's class ids, in order.
var modelLabels = []string{"person", "car", "bicycle"}

// modelChannel is each label's RGB channel.
var modelChannel = map[string]int{"person": 0, "car": 2, "bicycle": 1}

func startModelServer(harness *test_case_harness.TestCaseHarness, width, height int) (*modelServer, error) {
	m := &modelServer{
		harness: harness, width: width, height: height,
		maxBatch: 16, instances: 2, latency: 5 * time.Millisecond,
	}
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
	mux.HandleFunc("GET /v2/models/"+modelName, m.metadata)
	mux.HandleFunc("GET /v2/models/"+modelName+"/ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /v2/models/"+modelName+"/config", m.config)
	mux.HandleFunc("POST /v2/models/"+modelName+"/infer", m.infer)
	mux.HandleFunc("POST /v2/models/"+modelName+"/versions/{version}/infer", m.infer)
	m.mu.Lock()
	m.port = port
	m.listener = listener
	m.server = &http.Server{Handler: mux}
	m.slots = make(chan struct{}, m.instances)
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
	m.slots = make(chan struct{}, instances)
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
	m.stats = modelStats{inFlight: inFlight}
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

func (m *modelServer) metadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":     modelName,
		"versions": []string{"1"},
		"platform": "framewise",
		"inputs": []map[string]any{
			{"name": "images", "datatype": "UINT8", "shape": []int{-1, m.height, m.width, 3}},
		},
		"outputs": []map[string]any{
			{"name": "detections", "datatype": "FP32", "shape": []int{-1, len(modelLabels), 6}},
		},
	})
}

func (m *modelServer) config(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	maxBatch, instances := m.maxBatch, m.instances
	m.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"name":           modelName,
		"platform":       "framewise",
		"max_batch_size": maxBatch,
		"input": []map[string]any{
			{"name": "images", "data_type": "TYPE_UINT8", "format": "FORMAT_NHWC", "dims": []int{m.height, m.width, 3}},
		},
		"output": []map[string]any{
			{"name": "detections", "data_type": "TYPE_FP32", "dims": []int{len(modelLabels), 6},
				"label_filename": "labels.txt"},
		},
		"instance_group": []map[string]any{{"count": instances, "kind": "KIND_CPU"}},
		"parameters": map[string]any{
			"labels":         map[string]string{"string_value": "person,car,bicycle"},
			"pixel_format":   map[string]string{"string_value": "RGB"},
			"detection_row":  map[string]string{"string_value": "class_id,score,x,y,w,h (class_id -1: no box)"},
			"box_coordinate": map[string]string{"string_value": "pixels of the model's input"},
		},
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
	frames, batch, err := m.frames(req, data, maxBatch)
	if err != nil {
		m.count(func(s *modelStats) { s.rejected++ })
		modelError(w, 400, "%v", err)
		return
	}

	// Wait for an instance, then pay for the batch.
	select {
	case slots <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	time.Sleep(latency + time.Duration(batch)*perFrame)
	<-slots

	out := make([]float32, 0, batch*len(modelLabels)*6)
	for i, frame := range frames {
		if isGreyFrame(frame, m.width, m.height) {
			m.count(func(s *modelStats) { s.failed++ })
			modelError(w, 500, "inference failed: the fwdetect backend crashed on image %d of the batch (a uniform grey frame)", i)
			return
		}
		for _, label := range modelLabels {
			out = append(out, findBox(frame, m.width, m.height, label)...)
		}
	}
	m.count(func(s *modelStats) {
		s.ok++
		s.frames += batch
		s.batches = append(s.batches, batch)
		s.last = time.Now()
	})

	shape := []int{batch, len(modelLabels), 6}
	if wantsBinary(req) {
		raw := new(bytes.Buffer)
		binary.Write(raw, binary.LittleEndian, out)
		header, _ := json.Marshal(map[string]any{
			"model_name": modelName, "model_version": "1", "id": req.ID,
			"outputs": []map[string]any{{"name": "detections", "datatype": "FP32", "shape": shape,
				"parameters": map[string]any{"binary_data_size": raw.Len()}}},
		})
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Inference-Header-Content-Length", strconv.Itoa(len(header)))
		w.WriteHeader(200)
		w.Write(header)
		w.Write(raw.Bytes())
		return
	}
	writeJSON(w, 200, map[string]any{
		"model_name": modelName, "model_version": "1", "id": req.ID,
		"outputs": []map[string]any{{"name": "detections", "datatype": "FP32", "shape": shape, "data": out}},
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

// frames checks the input tensor and returns its frames (each H*W*3 RGB bytes).
func (m *modelServer) frames(req inferRequest, data []byte, maxBatch int) ([][]byte, int, error) {
	if len(req.Inputs) != 1 || req.Inputs[0].Name != "images" {
		var names []string
		for _, in := range req.Inputs {
			names = append(names, in.Name)
		}
		return nil, 0, fmt.Errorf("expected one input named \"images\", got %v", names)
	}
	in := req.Inputs[0]
	if in.Datatype != "UINT8" {
		return nil, 0, fmt.Errorf("input \"images\" must be UINT8, got %q", in.Datatype)
	}
	if len(in.Shape) != 4 || in.Shape[1] != m.height || in.Shape[2] != m.width || in.Shape[3] != 3 {
		return nil, 0, fmt.Errorf("input \"images\" must have shape [batch, %d, %d, 3], got %v", m.height, m.width, in.Shape)
	}
	batch := in.Shape[0]
	if batch < 1 || batch > maxBatch {
		return nil, 0, fmt.Errorf("batch size %d is outside this model's [1, %d] (max_batch_size in its config)", batch, maxBatch)
	}
	size := m.width * m.height * 3
	var pixels []byte
	if size64, ok := in.Parameters["binary_data_size"]; ok {
		n, _ := size64.(float64)
		if int(n) != len(data) {
			return nil, 0, fmt.Errorf("binary_data_size %d doesn't match the %d bytes after the header", int(n), len(data))
		}
		pixels = data
	} else {
		var err error
		if pixels, err = flattenUint8(in.Data); err != nil {
			return nil, 0, err
		}
	}
	if len(pixels) != batch*size {
		return nil, 0, fmt.Errorf("input \"images\" of shape %v needs %d bytes, got %d", in.Shape, batch*size, len(pixels))
	}
	frames := make([][]byte, batch)
	for i := range frames {
		frames[i] = pixels[i*size : (i+1)*size]
	}
	return frames, batch, nil
}

func flattenUint8(v any) ([]byte, error) {
	var out []byte
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
			if x < 0 || x > 255 || x != math.Trunc(x) {
				return fmt.Errorf("input \"images\" holds %v, which isn't a UINT8", x)
			}
			out = append(out, byte(x))
		case nil:
			return errors.New("input \"images\" has no data: send it as JSON \"data\" or as binary (binary_data_size)")
		default:
			return fmt.Errorf("input \"images\" data must be numbers, got %T", v)
		}
		return nil
	}
	return out, walk(v)
}

// findBox is fwdetect's rule on an RGB frame: the bounding box of the pixels
// where the label's channel is bright. Returns (class_id, score, x, y, w, h).
func findBox(frame []byte, width, height int, label string) []float32 {
	channel := modelChannel[label]
	x0, y0, x1, y1, count := width, height, -1, -1, 0
	for y := 0; y < height; y++ {
		row := frame[y*width*3:]
		for x := 0; x < width; x++ {
			if row[x*3+channel] > 160 {
				count++
				x0, x1, y0, y1 = min(x0, x), max(x1, x), min(y0, y), max(y1, y)
			}
		}
	}
	if count < 16 {
		return []float32{-1, 0, 0, 0, 0, 0}
	}
	w, h := x1-x0+1, y1-y0+1
	id := 0
	for i, l := range modelLabels {
		if l == label {
			id = i
		}
	}
	score := math.Round(float64(count)/float64(w*h)*1000) / 1000
	return []float32{float32(id), float32(score), float32(x0), float32(y0), float32(w), float32(h)}
}

// isGreyFrame is fwdetect's dropped-feed test: every 8th pixel near grey 128, with little spread.
func isGreyFrame(frame []byte, width, height int) bool {
	var sum, sumSq float64
	n := 0
	for y := 0; y < height; y += 8 {
		for x := 0; x < width; x += 8 {
			for c := 0; c < 3; c++ {
				v := float64(frame[(y*width+x)*3+c])
				sum += v
				sumSq += v * v
				n++
			}
		}
	}
	mean := sum / float64(n)
	std := math.Sqrt(max(0, sumSq/float64(n)-mean*mean))
	return math.Abs(mean-128) < 12 && std < 6
}
