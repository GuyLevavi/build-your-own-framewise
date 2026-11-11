package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
	"gopkg.in/yaml.v3"
)

// A small GitLab CI emulator: enough of .gitlab-ci.yml to run a job's script in its image,
// with its services beside it, the way a Docker-executor runner does. Not covered: include,
// extends, rules evaluation (the stage picks the jobs), artifacts and after_script.

type ciService struct {
	image string
	alias string
}

type ciJob struct {
	name         string
	stage        string
	image        string
	services     []ciService
	beforeScript []string
	script       []string
	variables    map[string]string
	cachePaths   []string
	needs        []string
	raw          map[string]any
}

type ciConfig struct {
	stages    []string
	variables map[string]string
	jobs      map[string]*ciJob
	order     []string // job names in file order
}

var ciReserved = map[string]bool{
	"stages": true, "variables": true, "default": true, "include": true, "workflow": true,
	"image": true, "services": true, "before_script": true, "after_script": true, "cache": true,
}

const ciFile = ".gitlab-ci.yml"

func loadCI(dir string) (*ciConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, ciFile))
	if err != nil {
		return nil, fmt.Errorf("expected a %s at the root of your repository, couldn't read one: %v", ciFile, err)
	}
	var top yaml.Node
	if err := yaml.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%s isn't valid YAML: %v", ciFile, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s isn't valid YAML: %v", ciFile, err)
	}
	if doc == nil {
		return nil, fmt.Errorf("%s is empty", ciFile)
	}
	if _, ok := doc["include"]; ok {
		return nil, fmt.Errorf("%s uses include:, which the tester can't follow. Keep the jobs in this one file", ciFile)
	}
	cfg := &ciConfig{jobs: map[string]*ciJob{}, variables: variablesOf(doc["variables"])}
	cfg.stages = stringList(doc["stages"])
	if len(cfg.stages) == 0 {
		cfg.stages = []string{"build", "test", "deploy"} // GitLab's default
	}
	defaults, _ := doc["default"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	for _, key := range []string{"image", "services", "before_script", "cache"} {
		if v, ok := doc[key]; ok {
			if _, set := defaults[key]; !set {
				defaults[key] = v
			}
		}
	}
	// File order of the jobs, from the YAML node tree.
	if len(top.Content) > 0 && top.Content[0].Kind == yaml.MappingNode {
		for i := 0; i+1 < len(top.Content[0].Content); i += 2 {
			name := top.Content[0].Content[i].Value
			if ciReserved[name] || strings.HasPrefix(name, ".") {
				continue
			}
			cfg.order = append(cfg.order, name)
		}
	}
	for _, name := range cfg.order {
		raw, ok := doc[name].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := raw["extends"]; ok {
			return nil, fmt.Errorf("job %q uses extends:, which the tester can't follow. Use a YAML anchor instead", name)
		}
		job := &ciJob{name: name, raw: raw, stage: "test", variables: variablesOf(raw["variables"])}
		if s, ok := raw["stage"].(string); ok {
			job.stage = s
		}
		get := func(key string) any {
			if v, ok := raw[key]; ok {
				return v
			}
			return defaults[key]
		}
		job.image = imageName(get("image"))
		job.beforeScript = scriptLines(get("before_script"))
		job.script = scriptLines(raw["script"])
		for _, key := range []string{"script", "before_script"} {
			if bad := nonStringLine(get(key)); bad != "" {
				return nil, fmt.Errorf("job %q: a %s line parsed as %s, not a command. YAML reads an unquoted \": \" as a map: quote the whole line", name, key, bad)
			}
		}
		for _, s := range listOf(get("services")) {
			svc := ciService{image: imageName(s)}
			if m, ok := s.(map[string]any); ok {
				svc.alias, _ = m["alias"].(string)
			}
			job.services = append(job.services, svc)
		}
		job.cachePaths = cachePaths(get("cache"))
		for _, n := range listOf(raw["needs"]) {
			switch v := n.(type) {
			case string:
				job.needs = append(job.needs, v)
			case map[string]any:
				if j, ok := v["job"].(string); ok {
					job.needs = append(job.needs, j)
				}
			}
		}
		cfg.jobs[name] = job
	}
	return cfg, nil
}

func listOf(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case nil:
		return nil
	default:
		return []any{t}
	}
}

func stringList(v any) []string {
	var result []string
	for _, item := range listOf(v) {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// scriptLines flattens a script: nested lists (from anchors) are allowed, as in GitLab.
func scriptLines(v any) []string {
	var result []string
	for _, item := range listOf(v) {
		switch t := item.(type) {
		case string:
			result = append(result, t)
		case []any:
			result = append(result, scriptLines(t)...)
		}
	}
	return result
}

// nonStringLine describes the first script entry that isn't a string (or a list of them).
func nonStringLine(v any) string {
	for _, item := range listOf(v) {
		switch t := item.(type) {
		case string:
		case []any:
			if bad := nonStringLine(t); bad != "" {
				return bad
			}
		default:
			return truncate(fmt.Sprintf("%v", t))
		}
	}
	return ""
}

func imageName(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s, _ := t["name"].(string)
		return s
	}
	return ""
}

func variablesOf(v any) map[string]string {
	result := map[string]string{}
	m, _ := v.(map[string]any)
	for k, val := range m {
		switch t := val.(type) {
		case map[string]any: // {value: ..., description: ...}
			result[k] = fmt.Sprint(t["value"])
		case nil:
			result[k] = ""
		default:
			result[k] = fmt.Sprint(t)
		}
	}
	return result
}

func cachePaths(v any) []string {
	var paths []string
	for _, c := range listOf(v) {
		if m, ok := c.(map[string]any); ok {
			paths = append(paths, stringList(m["paths"])...)
		}
	}
	return paths
}

// stageJobs lists the jobs of a stage in file order.
func (c *ciConfig) stageJobs(stage string) []*ciJob {
	var result []*ciJob
	for _, name := range c.order {
		if j, ok := c.jobs[name]; ok && j.stage == stage {
			result = append(result, j)
		}
	}
	return result
}

func (c *ciConfig) stageIndex(stage string) int {
	for i, s := range c.stages {
		if s == stage {
			return i
		}
	}
	return -1
}

var varPattern = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

// expand replaces $VAR and ${VAR} from vars, the way GitLab expands variables in image names.
func expand(s string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := varPattern.FindStringSubmatch(m)[1]
		if strings.HasPrefix(m, "${") && !strings.HasSuffix(m, "}") {
			return m
		}
		return vars[name]
	})
}

// projectVariables are what a project's CI/CD settings would define, with values for this machine.
func projectVariables() map[string]string {
	return map[string]string{
		"CI_IMAGE":       ciImage(),
		"POSTGRES_IMAGE": postgresImage(),
		"BASE_IMAGE":     baseImage(),
	}
}

// jobVariables merges, as GitLab does: predefined < project < file-level < job-level.
func (c *ciConfig) jobVariables(job *ciJob, predefined map[string]string) map[string]string {
	vars := map[string]string{}
	for _, layer := range []map[string]string{predefined, projectVariables(), c.variables, job.variables} {
		for k, v := range layer {
			vars[k] = v
		}
	}
	// One round of expansion, so FOO: ${CI_PROJECT_DIR}/x works.
	for k, v := range vars {
		vars[k] = expand(v, vars)
	}
	return vars
}

// ciRun is the outcome of one job.
type ciRun struct {
	exitCode int
	output   string
}

// runCIJob runs job in its image on a copy of workspace, with its services in the same pod,
// the way GitLab's Docker executor does. The cache paths are kept between runs on this machine.
func runCIJob(harness *test_case_harness.TestCaseHarness, c *ciConfig, job *ciJob, workspace string, predefined map[string]string) (ciRun, error) {
	const projectDir = "/builds/framewise"
	pre := map[string]string{
		"CI": "true", "GITLAB_CI": "true", "CI_PROJECT_DIR": projectDir, "CI_BUILDS_DIR": "/builds",
		"CI_JOB_NAME": job.name, "CI_JOB_STAGE": job.stage, "CI_DEFAULT_BRANCH": "main",
		"CI_PROJECT_NAME": "framewise", "CI_PROJECT_PATH": "team/framewise",
		"CI_SERVER_HOST": "gitlab.tester.invalid", "CI_SERVER_URL": "https://gitlab.tester.invalid",
	}
	for k, v := range predefined {
		pre[k] = v
	}
	vars := c.jobVariables(job, pre)
	image := expand(job.image, vars)
	if image == "" {
		return ciRun{}, fmt.Errorf("job %q has no image: set default: image: (the tester's CI_IMAGE is %s)", job.name, ciImage())
	}
	if len(job.script) == 0 {
		return ciRun{}, fmt.Errorf("job %q has no script", job.name)
	}

	pod := fmt.Sprintf("framewise-ci-%d", random.RandomInt(10000, 99999))
	podArgs := []string{"pod", "create", "--name", pod}
	for _, svc := range job.services {
		for _, alias := range serviceAliases(expand(svc.image, vars), svc.alias) {
			podArgs = append(podArgs, "--add-host", alias+":127.0.0.1")
		}
	}
	if out, err := podman(podArgs...); err != nil {
		return ciRun{}, fmt.Errorf("CodeCrafters Internal Error: podman pod create: %v\n%s", err, out)
	}
	defer podman("pod", "rm", "-f", "-t", "0", pod)

	envArgs := func() []string {
		var args []string
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			args = append(args, "-e", k+"="+vars[k])
		}
		return args
	}

	// Services get the job's variables, as in GitLab (so POSTGRES_PASSWORD reaches Postgres).
	for _, svc := range job.services {
		svcImage := expand(svc.image, vars)
		harness.Logger.Infof("  service %s (%s)", svcImage, strings.Join(serviceAliases(svcImage, svc.alias), ", "))
		args := append([]string{"run", "-d", "--pod", pod}, envArgs()...)
		args = append(args, svcImage)
		if out, err := podman(args...); err != nil {
			return ciRun{}, fmt.Errorf("job %q: service %s didn't start: %v\n%s", job.name, svcImage, err, tail(out, 10))
		}
	}
	if len(job.services) > 0 {
		waitServices(pod, 60*time.Second)
	}

	scriptDir, err := os.MkdirTemp("", "framewise-ci-script-")
	if err != nil {
		return ciRun{}, err
	}
	defer os.RemoveAll(scriptDir)
	var script strings.Builder
	script.WriteString("set -e\n")
	for _, line := range append(append([]string{}, job.beforeScript...), job.script...) {
		fmt.Fprintf(&script, "printf '%%s\\n' %s\n", shellQuote("$ "+line))
		script.WriteString(line + "\n")
	}
	writeFile(filepath.Join(scriptDir, "job.sh"), script.String())

	args := []string{"run", "--rm", "--pod", pod, "--entrypoint", "sh",
		"-v", workspace + ":" + projectDir + ":Z", "-v", scriptDir + ":/framewise-tester:ro,Z", "-w", projectDir}
	for _, p := range job.cachePaths {
		p = expand(p, vars)
		rel := strings.TrimPrefix(strings.TrimPrefix(p, projectDir), "/")
		if rel == "" || strings.Contains(rel, "..") || strings.ContainsAny(rel, "*?") {
			continue
		}
		host := filepath.Join(ciCacheDir(), strings.ReplaceAll(rel, "/", "_"))
		os.MkdirAll(host, 0o755)
		os.MkdirAll(filepath.Join(workspace, rel), 0o755)
		args = append(args, "-v", host+":"+projectDir+"/"+rel+":Z")
	}
	args = append(args, envArgs()...)
	args = append(args, image, "/framewise-tester/job.sh")
	harness.Logger.Infof("$ (job %s, stage %s, image %s)", job.name, job.stage, image)
	start := time.Now()
	out, err := command("", nil, 15*time.Minute, "podman", args...)
	run := ciRun{output: out}
	if err != nil {
		if exit, ok := err.(interface{ ExitCode() int }); ok {
			run.exitCode = exit.ExitCode()
		} else {
			return run, fmt.Errorf("job %q: %v\n%s", job.name, err, tail(out, 20))
		}
	}
	if run.exitCode == 125 { // podman's own failure: the image is missing, a bad option...
		return run, fmt.Errorf("job %q couldn't start in image %s:\n%s", job.name, image, tail(out, 10))
	}
	harness.Logger.Infof("  job %s exited with %d after %.0f s", job.name, run.exitCode, time.Since(start).Seconds())
	return run, nil
}

// serviceAliases are the hostnames a service is reachable at: its alias, or names derived
// from the image (postgres:16 → postgres), as GitLab does.
func serviceAliases(image, alias string) []string {
	if alias != "" {
		return []string{alias}
	}
	name := image
	if i := strings.LastIndex(name, "@"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name = name[:i]
	}
	aliases := []string{strings.NewReplacer("/", "__").Replace(name), strings.NewReplacer("/", "-").Replace(name)}
	last := name[strings.LastIndex(name, "/")+1:]
	return append(aliases, last)
}

// waitServices waits until every port the pod's services expose accepts a connection,
// as the runner's wait-for-service does. Timeouts are left for the job to report.
func waitServices(pod string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	ports := servicePorts(pod)
	for _, port := range ports {
		for time.Now().Before(deadline) {
			// A short-lived container in the pod shares its network, so 127.0.0.1 is the service.
			if _, err := podman("run", "--rm", "--pod", pod, "--entrypoint", "sh", ciImage(), "-c",
				fmt.Sprintf("python3 -c 'import socket; socket.create_connection((\"127.0.0.1\", %d), 1)'", port)); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
	}
}

// servicePorts lists the TCP ports the pod's containers' images expose.
func servicePorts(pod string) []int {
	out, err := podman("ps", "-a", "--pod", "--filter", "pod="+pod, "--format", "{{.Image}}")
	if err != nil {
		return nil
	}
	var ports []int
	for _, image := range lines(out) {
		if strings.Contains(image, "podman-pause") || strings.HasPrefix(image, "localhost/podman-pause") {
			continue
		}
		exposed, err := podman("image", "inspect", "--format", "{{range $p, $_ := .Config.ExposedPorts}}{{$p}} {{end}}", image)
		if err != nil {
			continue
		}
		for _, p := range strings.Fields(exposed) {
			var n int
			if _, err := fmt.Sscanf(p, "%d/tcp", &n); err == nil {
				ports = append(ports, n)
			}
		}
	}
	return ports
}

func ciCacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	dir := filepath.Join(base, "framewise-tester", "ci-cache")
	os.MkdirAll(dir, 0o755)
	return dir
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
