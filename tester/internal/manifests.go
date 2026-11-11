package internal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Kubernetes manifests, as the pod YAML (im2) and the Helm chart (hm1, hm2) produce them.

type kobj = map[string]any

// parseDocuments reads a multi-document YAML stream; empty documents are skipped.
func parseDocuments(data []byte) ([]kobj, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var docs []kobj
	for {
		var doc kobj
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		if doc != nil {
			docs = append(docs, doc)
		}
	}
}

// at walks nested maps and lists: at(o, "spec", "template", "spec").
func at(v any, path ...any) any {
	for _, p := range path {
		switch key := p.(type) {
		case string:
			m, ok := v.(kobj)
			if !ok {
				return nil
			}
			v = m[key]
		case int:
			l, ok := v.([]any)
			if !ok || key >= len(l) {
				return nil
			}
			v = l[key]
		}
	}
	return v
}

func str(v any, path ...any) string {
	s, _ := at(v, path...).(string)
	return s
}

func objects(v any, path ...any) []kobj {
	var result []kobj
	for _, item := range listOf(at(v, path...)) {
		if m, ok := item.(kobj); ok {
			result = append(result, m)
		}
	}
	return result
}

func kindOf(o kobj) string { return str(o, "kind") }
func nameOf(o kobj) string { return str(o, "metadata", "name") }

// podSpec is the pod spec inside a Pod, Deployment, StatefulSet or Job.
func podSpec(o kobj) kobj {
	if kindOf(o) == "Pod" {
		s, _ := at(o, "spec").(kobj)
		return s
	}
	s, _ := at(o, "spec", "template", "spec").(kobj)
	return s
}

func containersOf(o kobj) []kobj { return objects(podSpec(o), "containers") }

// commandLine is a container's command and args, joined, for finding `serve` or `work`.
func commandLine(c kobj) string {
	var parts []string
	for _, key := range []string{"command", "args"} {
		for _, item := range listOf(c[key]) {
			parts = append(parts, fmt.Sprint(item))
		}
	}
	return strings.Join(parts, " ")
}

// runsCommand reports whether the container's command line has word as a word.
func runsCommand(c kobj, word string) bool {
	for _, field := range strings.FieldsFunc(commandLine(c), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '&' || r == '|' || r == '"' || r == '\''
	}) {
		if field == word {
			return true
		}
	}
	return false
}

// argAfter returns the value after flag in the command line (`--port 8000` or `--port=8000`).
func argAfter(c kobj, flag string) string {
	fields := strings.Fields(commandLine(c))
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return strings.Trim(fields[i+1], `"'`)
		}
		if strings.HasPrefix(f, flag+"=") {
			return strings.Trim(strings.TrimPrefix(f, flag+"="), `"'`)
		}
	}
	return ""
}

// findContainer returns the first container (and its workload) that runs word.
func findContainer(docs []kobj, word string) (kobj, kobj) {
	for _, d := range docs {
		for _, c := range containersOf(d) {
			if runsCommand(c, word) {
				return d, c
			}
		}
	}
	return nil, nil
}

// envVar returns a container's env entry by name.
func envVar(c kobj, name string) kobj {
	for _, e := range objects(c, "env") {
		if str(e, "name") == name {
			return e
		}
	}
	return nil
}

// helmTemplate renders the chart in dir with extra arguments, and parses the output.
func helmTemplate(dir string, args ...string) ([]kobj, string, error) {
	all := append([]string{"template", "fw", "deploy/chart", "-f", "deploy/chart/values-nonprod.yaml"}, args...)
	out, err := command(dir, nil, 2*time.Minute, "helm", all...)
	if err != nil {
		return nil, out, fmt.Errorf("helm %s failed: %v\n%s", strings.Join(all, " "), err, tail(out, 20))
	}
	docs, err := parseDocuments([]byte(out))
	if err != nil {
		return nil, out, fmt.Errorf("helm template printed YAML that doesn't parse: %v", err)
	}
	return docs, out, nil
}

// appImages are the images of the containers that run serve, work or migrate.
func appImages(docs []kobj) map[string]string {
	images := map[string]string{}
	for _, d := range docs {
		for _, c := range append(containersOf(d), objects(podSpec(d), "initContainers")...) {
			for _, word := range []string{"serve", "work", "migrate"} {
				if runsCommand(c, word) {
					images[kindOf(d)+"/"+nameOf(d)+" ("+word+")"] = str(c, "image")
				}
			}
		}
	}
	return images
}

func imageTag(image string) string {
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		return image[i+1:]
	}
	return "latest"
}

func readYAMLFile(path string) (kobj, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc kobj
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s isn't valid YAML: %v", filepath.Base(path), err)
	}
	return doc, nil
}
