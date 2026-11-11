package internal

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// What the course's model reports on every frame of every scene, recorded by
// tester/golden/make_golden.py (see its header): the detector's boxes in the
// video's pixels and the classifier's top class, each with its score.
//
//go:embed golden
var goldenFiles embed.FS

type golden struct {
	Detect   [][]box
	Classify []box // Label and Score only
}

type goldenFile struct {
	Scene    string          `json:"scene"`
	Frames   int             `json:"frames"`
	Detect   [][][]any       `json:"detect"`   // per frame: [label, score, x, y, w, h]
	Classify [][]any         `json:"classify"` // per frame: [label, score]
	Extra    json.RawMessage `json:"generator"`
}

var (
	goldenMu    sync.Mutex
	goldenCache = map[string]*golden{}
)

func goldenFor(s scene) *golden {
	goldenMu.Lock()
	defer goldenMu.Unlock()
	if g, ok := goldenCache[s.name]; ok {
		return g
	}
	g, err := loadGolden(s)
	if err != nil {
		panic(fmt.Sprintf("CodeCrafters Internal Error: golden file for %s: %v", s.name, err))
	}
	goldenCache[s.name] = g
	return g
}

func loadGolden(s scene) (*golden, error) {
	data, err := goldenFiles.ReadFile("golden/" + s.name + ".json.gz")
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	var f goldenFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if f.Frames != s.frames || len(f.Detect) != s.frames || len(f.Classify) != s.frames {
		return nil, fmt.Errorf("it has %d frames, the scene %d: run tester/golden/make_golden.py", len(f.Detect), s.frames)
	}
	g := &golden{Detect: make([][]box, s.frames), Classify: make([]box, s.frames)}
	for i, rows := range f.Detect {
		for _, r := range rows {
			if len(r) != 6 {
				return nil, fmt.Errorf("frame %d: bad row %v", i, r)
			}
			g.Detect[i] = append(g.Detect[i], box{
				Label: r[0].(string), Score: r[1].(float64),
				X: int(r[2].(float64)), Y: int(r[3].(float64)), W: int(r[4].(float64)), H: int(r[5].(float64)),
			})
		}
	}
	for i, r := range f.Classify {
		if len(r) != 2 {
			return nil, fmt.Errorf("frame %d: bad class %v", i, r)
		}
		g.Classify[i] = box{Label: r[0].(string), Score: r[1].(float64)}
	}
	return g, nil
}
