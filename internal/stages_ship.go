package internal

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codecrafters-io/tester-utils/random"
	"github.com/codecrafters-io/tester-utils/test_case_harness"
)

// The delivery stages (GitLab CI, images, the pod, the Helm
// chart and release by commit), checked on one offline machine with podman, helm and git.
// What needs the real services (pushing to Quay, an Argo CD sync, OpenShift) stays with the
// assignment's review.

// ---- ci1: Gate merges

// mergeRequestVariables are what GitLab sets in a merge request pipeline.
func mergeRequestVariables() map[string]string {
	return map[string]string{
		"CI_PIPELINE_SOURCE": "merge_request_event", "CI_MERGE_REQUEST_IID": "1",
		"CI_MERGE_REQUEST_TARGET_BRANCH_NAME": "main", "CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feature",
		"CI_COMMIT_REF_NAME": "feature", "CI_COMMIT_REF_SLUG": "feature",
		"CI_COMMIT_SHA": "0123456789abcdef0123456789abcdef01234567", "CI_COMMIT_SHORT_SHA": "01234567",
	}
}

// onlyOnDefaultBranch reports whether a job's rules or only: keep it to the default branch.
func onlyOnDefaultBranch(job *ciJob) bool {
	text := fmt.Sprint(job.raw["rules"]) + " " + fmt.Sprint(job.raw["only"])
	return containsAny(text, "CI_DEFAULT_BRANCH", "== \"main\"", "== 'main'", "== main", "[main]", "== \"master\"", "[master]")
}

func testGateMerges(harness *test_case_harness.TestCaseHarness) error {
	if _, err := needTool("podman", "it runs your CI jobs in containers, as a GitLab runner does"); err != nil {
		return err
	}
	dir := repoDir(harness)
	cfg, err := loadCI(dir)
	if err != nil {
		return err
	}
	lint, test := cfg.stageIndex("lint"), cfg.stageIndex("test")
	if lint < 0 || test < 0 {
		return fmt.Errorf("expected stages: in %s to list lint and test, got %v", ciFile, cfg.stages)
	}
	if lint > test {
		return fmt.Errorf("expected the lint stage before the test stage (a lint failure is the cheapest to find), got %v", cfg.stages)
	}
	// Every job up to and including the test stage gates the merge request.
	var gate []*ciJob
	for _, stage := range cfg.stages[:test+1] {
		gate = append(gate, cfg.stageJobs(stage)...)
	}
	for _, stage := range []string{"lint", "test"} {
		if len(cfg.stageJobs(stage)) == 0 {
			return fmt.Errorf("expected a job in the %s stage, found none", stage)
		}
	}
	names := make([]string, len(gate))
	for i, j := range gate {
		names[i] = j.name
		if onlyOnDefaultBranch(j) {
			return fmt.Errorf("job %q only runs on the default branch, so it can't block a merge request. Run it on merge requests too", j.name)
		}
	}
	harness.Logger.Successf("✓ %s: stages %s; jobs that gate a merge request: %s", ciFile, strings.Join(cfg.stages, ", "), strings.Join(names, ", "))

	// The uv cache: a folder inside the project, listed in cache: paths.
	for _, j := range gate {
		vars := cfg.jobVariables(j, mergeRequestVariables())
		uvCache := strings.TrimPrefix(strings.TrimPrefix(vars["UV_CACHE_DIR"], "/builds/framewise"), "/")
		uvCache = strings.TrimSuffix(strings.TrimPrefix(uvCache, "./"), "/")
		if uvCache == "" {
			return fmt.Errorf("job %q: expected UV_CACHE_DIR to be set to a folder inside the project (for example .uv-cache), so the runner can cache it. Without it every pipeline downloads every package again", j.name)
		}
		cached := false
		for _, p := range j.cachePaths {
			p = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(expand(p, vars), "/builds/framewise/"), "./"), "/")
			if p == uvCache {
				cached = true
			}
		}
		if !cached {
			return fmt.Errorf("job %q: UV_CACHE_DIR is %s, but cache: paths doesn't list it (got %v)", j.name, uvCache, j.cachePaths)
		}
	}
	harness.Logger.Successf("✓ the uv cache is kept between pipelines")

	runAll := func(title string, plant func(ws string), jobs []*ciJob) (map[string]ciRun, error) {
		harness.Logger.Infof("%s", title)
		ws := tempDir(harness)
		if err := copyRepo(dir, ws); err != nil {
			return nil, err
		}
		plant(ws)
		// A runner works in a clone, so tools that honour .gitignore see one.
		for _, args := range [][]string{{"init", "-q", "-b", "feature"}, {"add", "-A"}, {"commit", "-q", "-m", "The merge request"}} {
			if _, err := git(ws, args...); err != nil {
				return nil, err
			}
		}
		runs := map[string]ciRun{}
		for _, j := range jobs {
			r, err := runCIJob(harness, cfg, j, ws, mergeRequestVariables())
			if err != nil {
				return nil, err
			}
			runs[j.name] = r
		}
		return runs, nil
	}

	runs, err := runAll("Pipeline 1: a merge request with your code as it is", func(string) {}, gate)
	if err != nil {
		return err
	}
	for _, j := range gate {
		if r := runs[j.name]; r.exitCode != 0 {
			return fmt.Errorf("expected job %q to pass on your code, it failed with exit code %d:\n%s", j.name, r.exitCode, indent(tail(r.output, 30)))
		}
	}
	harness.Logger.Successf("✓ every job passed")

	appDir := "app"
	if _, err := os.Stat(filepath.Join(dir, "app")); err != nil {
		appDir = "."
	}
	runs, err = runAll("Pipeline 2: the same merge request plus a test that fails (tests/test_planted_by_tester.py)", func(ws string) {
		writeFile(filepath.Join(ws, "tests", "test_planted_by_tester.py"),
			"def test_planted_by_the_tester() -> None:\n    assert 1 + 1 == 3, \"planted by the tester: this must fail the pipeline\"\n")
	}, cfg.stageJobs("test"))
	if err != nil {
		return err
	}
	if err := expectAFailure(harness, runs, "test_planted_by_the_tester", "a failing test"); err != nil {
		return err
	}

	planted := filepath.Join(appDir, "planted_by_tester.py")
	runs, err = runAll(fmt.Sprintf("Pipeline 3: the same merge request plus an unused import (%s)", planted), func(ws string) {
		writeFile(filepath.Join(ws, planted), "import os\n")
	}, cfg.stageJobs("lint"))
	if err != nil {
		return err
	}
	return expectAFailure(harness, runs, "planted_by_tester", "an unused import")
}

// expectAFailure checks that some job failed, and that its output names what was planted.
func expectAFailure(harness *test_case_harness.TestCaseHarness, runs map[string]ciRun, mention, what string) error {
	names := make([]string, 0, len(runs))
	for name := range runs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		r := runs[name]
		if r.exitCode != 0 {
			if !strings.Contains(r.output, mention) {
				return fmt.Errorf("job %q failed, but its output doesn't mention %s, so it failed for another reason:\n%s", name, mention, indent(tail(r.output, 20)))
			}
			harness.Logger.Successf("✓ %s failed job %s, so the merge request is blocked", what, name)
			return nil
		}
	}
	var last string
	if len(names) > 0 {
		last = indent(tail(runs[names[len(names)-1]].output, 15))
	}
	return fmt.Errorf("expected %s to fail one of the jobs %s, but every one passed:\n%s", what, strings.Join(names, ", "), last)
}

// ---- im1: Build an image

const randomUID = "1000620000:0" // an OpenShift-style user: a random UID in group 0

var buildTools = map[string]bool{"gcc": true, "cc": true, "g++": true, "c++": true, "make": true, "clang": true, "ld": true}

// buildRepoImage copies the repository (plus plant, if any) to a build context and builds tag.
func buildRepoImage(harness *test_case_harness.TestCaseHarness, tag string, plant func(ctx string)) error {
	dir := repoDir(harness)
	file, err := containerfile(dir)
	if err != nil {
		return err
	}
	ctx := tempDir(harness)
	if err := copyRepo(dir, ctx); err != nil {
		return err
	}
	if plant != nil {
		plant(ctx)
	}
	return buildImage(harness, ctx, file, tag)
}

func testBuildImage(harness *test_case_harness.TestCaseHarness) error {
	if _, err := needTool("podman", "it builds and runs your image"); err != nil {
		return err
	}
	dir := repoDir(harness)
	file, err := containerfile(dir)
	if err != nil {
		return err
	}
	text, _ := os.ReadFile(filepath.Join(dir, file))
	froms := 0
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "FROM ") {
			froms++
		}
	}
	if froms < 2 {
		return fmt.Errorf("expected a multi-stage %s (a build stage and a runtime stage), found %d FROM line(s)", file, froms)
	}
	ignore := ""
	for _, name := range []string{".containerignore", ".dockerignore"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			ignore = name
		}
	}
	if ignore == "" {
		return fmt.Errorf("expected a .containerignore (or .dockerignore): without one the build context holds .git, .venv and whatever else is in the folder")
	}
	harness.Logger.Successf("✓ %s has %d stages; %s exists", file, froms, ignore)

	tag := fmt.Sprintf("localhost/framewise-tester:%d", random.RandomInt(10000, 99999))
	harness.RegisterTeardownFunc(func() { podman("rmi", "-f", tag) })
	harness.Logger.Infof("The tester adds .env, .git/, .venv/ and tests/ files to a copy of your repository; none may reach the image")
	err = buildRepoImage(harness, tag, func(ctx string) {
		writeFile(filepath.Join(ctx, ".env"), "FRAMEWISE_API_KEY=tester-planted-secret\n")
		writeFile(filepath.Join(ctx, ".git", "tester-planted"), "planted by the tester\n")
		writeFile(filepath.Join(ctx, ".venv", "tester-planted"), "planted by the tester\n")
		writeFile(filepath.Join(ctx, "tests", "tester-planted.py"), "# planted by the tester\n")
	})
	if err != nil {
		return err
	}
	info, err := inspectImage(tag)
	if err != nil {
		return err
	}
	harness.Logger.Infof("Image size: %.0f MiB", float64(info.Size)/(1<<20))

	files, err := imageFiles(tag)
	if err != nil {
		return err
	}
	var leaked, tools []string
	byName := map[string]imageFile{}
	for _, f := range files {
		byName[strings.TrimSuffix(f.name, "/")] = f
		base := filepath.Base(f.name)
		if strings.Contains(f.name, "tester-planted") || base == ".env" {
			leaked = append(leaked, "/"+f.name)
		}
		if d := filepath.Dir(f.name); (d == "usr/bin" || d == "usr/local/bin" || d == "bin") && buildTools[base] {
			tools = append(tools, "/"+f.name)
		}
	}
	if len(leaked) > 0 {
		return fmt.Errorf("expected the image to hold none of the planted files, found %s. List them in %s", strings.Join(leaked, ", "), ignore)
	}
	harness.Logger.Successf("✓ no .env, .git, .venv or tests in the image")
	if len(tools) > 0 {
		return fmt.Errorf("expected no build tools in the runtime image, found %s. Compile in the build stage and copy only the result", strings.Join(tools, ", "))
	}
	harness.Logger.Successf("✓ no compilers in the image")
	workdir := strings.Trim(info.Config.WorkingDir, "/")
	if workdir == "" {
		return fmt.Errorf("expected the image to set WORKDIR (the app's folder), it doesn't")
	}
	if f, ok := byName[workdir]; ok && (f.gid != 0 || f.mode&0o020 == 0) {
		return fmt.Errorf("expected WORKDIR /%s to belong to group 0 and be group-writable (OpenShift runs the container as a random UID in group 0), got group %d, mode %o. Use chgrp -R 0 and chmod -R g=u", workdir, f.gid, f.mode&0o777)
	}
	harness.Logger.Successf("✓ WORKDIR /%s belongs to group 0 and is group-writable", workdir)

	return runImage(harness, tag)
}

// runImage runs migrate, serve and work from the image as a random UID, and a job end to end.
func runImage(harness *test_case_harness.TestCaseHarness, tag string) error {
	dbURL, err := stageDatabase(harness)
	if err != nil {
		return err
	}
	profiles := writeProfiles(harness, testProfilesYAML)
	common := []string{"--network", "host", "--user", randomUID,
		"-e", "DATABASE_URL=" + dbURL, "-e", "FRAMEWISE_PROFILES=/etc/framewise-tester/profiles.yaml",
		"-v", profiles + ":/etc/framewise-tester/profiles.yaml:ro,Z"}

	m, err := runContainer(harness, "migrate", common, tag, "migrate")
	if err != nil {
		return err
	}
	code, err := m.waitExit(2 * time.Minute)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("expected `migrate` in the image to exit with 0 as user %s, got %d:\n%s", randomUID, code, indent(tail(m.logs(), 20)))
	}
	harness.Logger.Successf("✓ migrate exited with 0 as user %s", randomUID)

	volume := fmt.Sprintf("framewise-tester-data-%d", random.RandomInt(10000, 99999))
	if out, err := podman("volume", "create", volume); err != nil {
		return fmt.Errorf("CodeCrafters Internal Error: podman volume create: %v\n%s", err, out)
	}
	harness.RegisterTeardownFunc(func() { podman("volume", "rm", "-f", volume) })
	shared := append(append([]string{}, common...), "-v", volume+":/data:U")

	port := freePort()
	srv, err := runContainer(harness, "serve", shared, tag, "serve", "--port", fmt.Sprint(port), "--data-dir", "/data")
	if err != nil {
		return err
	}
	s := containerServer(harness, srv, fmt.Sprintf("http://127.0.0.1:%d", port))
	if err := waitHealthy(harness, s, 60*time.Second, "The image's ENTRYPOINT must be your program, so that `podman run IMAGE serve --port N` starts the server."); err != nil {
		return fmt.Errorf("%v\nServer container log:\n%s", err, indent(tail(srv.logs(), 20)))
	}
	wk, err := runContainer(harness, "work", shared, tag, "work")
	if err != nil {
		return err
	}
	if err := s.runJob(sceneStreet); err != nil {
		return fmt.Errorf("%v\nWorker container log:\n%s", err, indent(tail(wk.logs(), 20)))
	}
	wk.stop()
	if _, exit, _ := wk.state(); exit != 0 {
		return fmt.Errorf("expected the worker container to exit with 0 on SIGTERM, as the worker does outside one, got %d. In a container your program is PID 1: a shell-form ENTRYPOINT or a wrapper script without exec doesn't pass SIGTERM on", exit)
	}
	harness.Logger.Successf("✓ the worker container stopped cleanly on SIGTERM")
	return nil
}

// ---- im2: Run the pod

const podImage = "localhost/framewise:latest"

func testRunPod(harness *test_case_harness.TestCaseHarness) error {
	if _, err := needTool("podman", "it runs your pod with podman kube play"); err != nil {
		return err
	}
	dir := repoDir(harness)
	podFile := filepath.Join("deploy", "pod.yaml")
	data, err := os.ReadFile(filepath.Join(dir, podFile))
	if err != nil {
		return fmt.Errorf("expected %s, couldn't read it: %v", podFile, err)
	}
	docs, err := parseDocuments(data)
	if err != nil {
		return fmt.Errorf("%s isn't valid YAML: %v", podFile, err)
	}
	var pod kobj
	for _, d := range docs {
		if k := kindOf(d); k == "Pod" || k == "Deployment" {
			if pod != nil {
				return fmt.Errorf("expected one Pod in %s, found more: keep the API, the worker and Postgres in one pod", podFile)
			}
			pod = d
		}
	}
	if pod == nil {
		return fmt.Errorf("expected a Pod in %s, found none", podFile)
	}
	_, api := findContainer([]kobj{pod}, "serve")
	_, worker := findContainer([]kobj{pod}, "work")
	var pg kobj
	for _, c := range containersOf(pod) {
		if strings.Contains(str(c, "image"), "postgres") {
			pg = c
		}
	}
	switch {
	case api == nil:
		return fmt.Errorf("expected a container in the pod that runs `serve`, found none")
	case worker == nil:
		return fmt.Errorf("expected a container in the pod that runs `work`, found none")
	case pg == nil:
		return fmt.Errorf("expected a Postgres container in the pod (an image with postgres in its name), found none")
	}
	for _, c := range []kobj{api, worker} {
		if img := str(c, "image"); img != podImage && img != "framewise:latest" {
			return fmt.Errorf("expected container %q to use the image %s (the tester builds your Containerfile under that name), got %q", str(c, "name"), podImage, img)
		}
	}
	hostPort := 0
	for _, p := range objects(api, "ports") {
		if n, ok := at(p, "hostPort").(int); ok {
			hostPort = n
		}
	}
	if hostPort == 0 {
		return fmt.Errorf("expected container %q to publish the API with ports: [{containerPort: ..., hostPort: ...}], it has no hostPort", str(api, "name"))
	}
	harness.Logger.Successf("✓ %s: API in %q on host port %d, worker in %q, Postgres in %q", podFile, str(api, "name"), hostPort, str(worker, "name"), str(pg, "name"))

	if _, err := podman("image", "exists", str(pg, "image")); err != nil {
		return fmt.Errorf("the image %s isn't on this machine, and the tester works offline. Load it first (podman load, or podman pull where there is a registry)", str(pg, "image"))
	}
	if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", hostPort)); err != nil {
		return fmt.Errorf("host port %d is already in use; stop whatever holds it (an earlier `podman kube play`?) and run again", hostPort)
	} else {
		l.Close()
	}
	if err := buildRepoImage(harness, podImage, nil); err != nil {
		return err
	}

	harness.Logger.Infof("$ podman kube play --replace %s", podFile)
	out, err := command(dir, nil, 5*time.Minute, "podman", "kube", "play", "--replace", podFile)
	harness.RegisterTeardownFunc(func() { command(dir, nil, 2*time.Minute, "podman", "kube", "down", "--force", podFile) })
	if err != nil {
		return fmt.Errorf("podman kube play failed: %v\n%s", err, tail(out, 20))
	}
	podName := nameOf(pod)
	if kindOf(pod) == "Deployment" {
		podName += "-pod"
	}
	containerName := func(c kobj) string { return podName + "-" + str(c, "name") }
	apiC := &container{harness: harness, name: containerName(api)}
	workerC := &container{harness: harness, name: containerName(worker)}
	s := containerServer(harness, apiC, fmt.Sprintf("http://127.0.0.1:%d", hostPort))
	if err := waitHealthy(harness, s, 120*time.Second, "In a pod the published port reaches the pod's network interface, not its loopback: serve must listen on 0.0.0.0 there."); err != nil {
		return fmt.Errorf("%v\nAPI container log:\n%s", err, indent(tail(apiC.logs(), 20)))
	}

	deadline := time.Now().Add(90 * time.Second)
	for {
		r, err := s.send("GET", "/ready", "", nil, nil)
		if err == nil && r.status == 200 {
			harness.Logger.Successf("✓ GET /ready answered 200: the API reaches Postgres in the pod")
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected GET /ready to answer 200 within 90 s of the pod starting (Postgres is in the pod, on localhost), it didn't\nAPI container log:\n%s", indent(tail(apiC.logs(), 20)))
		}
		time.Sleep(time.Second)
	}

	runPodJob := func() error {
		videoID, err := s.uploadClip("street.avi", sceneStreet.bytes())
		if err != nil {
			return err
		}
		jobID, _, err := s.submitJob(videoID, "")
		if err != nil {
			return err
		}
		job, err := s.waitJob(jobID, 120*time.Second)
		if err != nil {
			return fmt.Errorf("%v\nWorker container log:\n%s", err, indent(tail(workerC.logs(), 20)))
		}
		if job["status"] != "DONE" {
			return fmt.Errorf("expected job %s to be DONE, got %v (error: %v)\nWorker container log:\n%s", jobID, job["status"], job["error"], indent(tail(workerC.logs(), 20)))
		}
		got, err := labelsField(job, "labels")
		if err != nil {
			return err
		}
		if len(got) == 0 {
			return fmt.Errorf("expected the street clip to have labels, got none")
		}
		harness.Logger.Successf("✓ DONE: %s", formatLabels(got))
		return nil
	}
	if err := runPodJob(); err != nil {
		return err
	}

	_, _, before := workerC.state()
	if err := workerC.crash(); err != nil {
		return err
	}
	deadline = time.Now().Add(60 * time.Second)
	for {
		running, _, restarts := workerC.state()
		if running && restarts > before {
			harness.Logger.Successf("✓ the pod restarted the worker")
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("expected the pod to restart the killed worker within 60 s, it didn't (restartPolicy: Always restarts a container whatever its exit code)")
		}
		time.Sleep(500 * time.Millisecond)
	}
	return runPodJob()
}

// ---- hm1: Chart it

const chartDir = "deploy/chart"

func chartFiles(dir string) error {
	for _, f := range []string{"Chart.yaml", "values.yaml", "values-nonprod.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, chartDir, f)); err != nil {
			return fmt.Errorf("expected %s/%s, it doesn't exist", chartDir, f)
		}
	}
	values, err := readYAMLFile(filepath.Join(dir, chartDir, "values-nonprod.yaml"))
	if err != nil {
		return err
	}
	if at(values, "image", "tag") == nil {
		return fmt.Errorf("expected image.tag in %s/values-nonprod.yaml: the release pipeline will change it there", chartDir)
	}
	return nil
}

func testChart(harness *test_case_harness.TestCaseHarness) error {
	if _, err := needTool("helm", "it lints and renders your chart"); err != nil {
		return err
	}
	dir := repoDir(harness)
	if err := chartFiles(dir); err != nil {
		return err
	}
	harness.Logger.Infof("$ helm lint %s -f %s/values-nonprod.yaml", chartDir, chartDir)
	if out, err := command(dir, nil, 2*time.Minute, "helm", "lint", chartDir, "-f", chartDir+"/values-nonprod.yaml"); err != nil {
		return fmt.Errorf("helm lint failed:\n%s", indent(tail(out, 20)))
	}
	harness.Logger.Successf("✓ helm lint passed")
	tag := fmt.Sprintf("tester-%d", random.RandomInt(100000, 999999))
	harness.Logger.Infof("$ helm template fw %s -f %s/values-nonprod.yaml --set image.tag=%s", chartDir, chartDir, tag)
	docs, out, err := helmTemplate(dir, "--set", "image.tag="+tag)
	if err != nil {
		return err
	}
	return checkManifests(harness, docs, out, tag)
}

// checkManifests holds the rendered chart to what the assignment asks of the cluster manifests.
func checkManifests(harness *test_case_harness.TestCaseHarness, docs []kobj, rendered, tag string) error {
	apiD, api := findContainer(docs, "serve")
	workerD, worker := findContainer(docs, "work")
	migrateD, _ := findContainer(docs, "migrate")
	switch {
	case apiD == nil || kindOf(apiD) != "Deployment":
		return fmt.Errorf("expected a Deployment whose container runs `serve`, found none")
	case workerD == nil || kindOf(workerD) != "Deployment":
		return fmt.Errorf("expected a Deployment whose container runs `work`, found none")
	case nameOf(apiD) == nameOf(workerD):
		return fmt.Errorf("expected the API and the worker in separate Deployments, so they scale separately")
	case migrateD == nil || kindOf(migrateD) != "Job":
		return fmt.Errorf("expected a Job that runs `migrate`, found none")
	}
	harness.Logger.Successf("✓ Deployments %s (serve) and %s (work), Job %s (migrate)", nameOf(apiD), nameOf(workerD), nameOf(migrateD))

	for where, image := range appImages(docs) {
		if imageTag(image) != tag {
			return fmt.Errorf("expected %s to use the image tag from values (%s), got %q", where, tag, image)
		}
	}
	harness.Logger.Successf("✓ every app container takes its image tag from image.tag")

	// Probes: liveness checks only the process, readiness may check the database.
	for _, probe := range []struct{ key, path string }{{"livenessProbe", "/health"}, {"readinessProbe", "/ready"}} {
		if p := str(api, probe.key, "httpGet", "path"); p != probe.path {
			return fmt.Errorf("expected the API's %s to be an httpGet on %s, got %q. Liveness must not depend on the database, or an outage restarts every pod", probe.key, probe.path, p)
		}
	}
	harness.Logger.Successf("✓ liveness on /health, readiness on /ready")
	for _, c := range []kobj{api, worker} {
		if at(c, "resources", "limits", "memory") == nil || at(c, "resources", "requests", "cpu") == nil {
			return fmt.Errorf("expected container %q to set resources: requests.cpu and limits.memory", str(c, "name"))
		}
	}
	harness.Logger.Successf("✓ requests and limits on the API and the worker")

	// The Service and the Route reach the API pods, and only them.
	port := argAfter(api, "--port")
	var svc kobj
	for _, d := range docs {
		if kindOf(d) != "Service" {
			continue
		}
		selector, _ := at(d, "spec", "selector").(kobj)
		if len(selector) > 0 && selects(selector, apiD) && !selects(selector, workerD) {
			svc = d
		}
	}
	if svc == nil {
		return fmt.Errorf("expected a Service whose selector matches the API pods and not the worker's")
	}
	if port != "" {
		found := false
		for _, p := range objects(svc, "spec", "ports") {
			if fmt.Sprint(at(p, "targetPort")) == port || (at(p, "targetPort") == nil && fmt.Sprint(at(p, "port")) == port) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("expected Service %s to target port %s, where serve listens", nameOf(svc), port)
		}
	}
	if !containsAny(commandLine(api), "0.0.0.0") {
		return fmt.Errorf("expected serve to listen on 0.0.0.0 in the cluster (--host 0.0.0.0): on 127.0.0.1 the Service can't reach it")
	}
	routed := false
	for _, d := range docs {
		if kindOf(d) == "Route" && str(d, "spec", "to", "name") == nameOf(svc) {
			routed = true
		}
	}
	if !routed {
		return fmt.Errorf("expected a Route (route.openshift.io/v1) to Service %s", nameOf(svc))
	}
	harness.Logger.Successf("✓ Service %s and a Route reach the API", nameOf(svc))

	// Secrets, config and storage.
	if containsAny(rendered, "postgresql://", "postgresql+psycopg://", "postgres://") {
		return fmt.Errorf("expected no database URL in the rendered chart: it holds a password. Create the Secret outside Git and reference it")
	}
	for _, d := range []kobj{apiD, workerD, migrateD} {
		for _, c := range containersOf(d) {
			if !runsCommand(c, "serve") && !runsCommand(c, "work") && !runsCommand(c, "migrate") {
				continue
			}
			e := envVar(c, "DATABASE_URL")
			if e == nil || at(e, "valueFrom", "secretKeyRef", "name") == nil {
				return fmt.Errorf("expected DATABASE_URL in %s %s to come from a Secret (valueFrom.secretKeyRef)", kindOf(d), nameOf(d))
			}
			if at(c, "securityContext", "runAsUser") != nil || at(podSpec(d), "securityContext", "runAsUser") != nil {
				return fmt.Errorf("expected %s %s not to set runAsUser: OpenShift picks the UID", kindOf(d), nameOf(d))
			}
		}
	}
	harness.Logger.Successf("✓ DATABASE_URL comes from a Secret; no runAsUser")

	profiles := ""
	for i, c := range []kobj{api, worker} {
		e := envVar(c, "FRAMEWISE_PROFILES")
		path := str(e, "value")
		mount, volume := mountFor(c, path)
		if path == "" || mount == "" {
			return fmt.Errorf("expected FRAMEWISE_PROFILES in container %q to point into a mounted ConfigMap", str(c, "name"))
		}
		d := apiD
		if i == 1 {
			d = workerD
		}
		v := volumeNamed(podSpec(d), volume)
		cm := str(v, "configMap", "name")
		if cm == "" {
			return fmt.Errorf("expected the volume holding %s in container %q to be a ConfigMap", path, str(c, "name"))
		}
		if !hasObject(docs, "ConfigMap", cm) {
			return fmt.Errorf("expected the chart to render ConfigMap %s, it doesn't", cm)
		}
		profiles = cm
	}
	harness.Logger.Successf("✓ the profiles come from ConfigMap %s", profiles)

	dataDir := argAfter(api, "--data-dir")
	apiMount, apiVolume := mountFor(api, dataDir)
	if dataDir == "" || apiMount == "" {
		return fmt.Errorf("expected serve's --data-dir to be on a mounted volume")
	}
	claim := str(volumeNamed(podSpec(apiD), apiVolume), "persistentVolumeClaim", "claimName")
	if claim == "" {
		return fmt.Errorf("expected the volume holding --data-dir to be a PersistentVolumeClaim")
	}
	workerShares := false
	for _, m := range objects(worker, "volumeMounts") {
		if str(m, "mountPath") == apiMount && str(volumeNamed(podSpec(workerD), str(m, "name")), "persistentVolumeClaim", "claimName") == claim {
			workerShares = true
		}
	}
	if !workerShares {
		return fmt.Errorf("expected the worker to mount claim %s at %s too: it reads the videos the API stored", claim, apiMount)
	}
	for _, d := range docs {
		if kindOf(d) == "PersistentVolumeClaim" && nameOf(d) == claim {
			if !containsAny(fmt.Sprint(at(d, "spec", "accessModes")), "ReadWriteMany") {
				return fmt.Errorf("expected claim %s to be ReadWriteMany: pods on different nodes share it", claim)
			}
			harness.Logger.Successf("✓ API and worker share ReadWriteMany claim %s at %s", claim, apiMount)
			return nil
		}
	}
	return fmt.Errorf("expected the chart to render PersistentVolumeClaim %s, it doesn't", claim)
}

// selects reports whether selector matches the workload's pod labels.
func selects(selector kobj, workload kobj) bool {
	labels, _ := at(workload, "spec", "template", "metadata", "labels").(kobj)
	for k, v := range selector {
		if fmt.Sprint(labels[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// mountFor finds the volume mount that holds path; it returns the mount path and volume name.
func mountFor(c kobj, path string) (string, string) {
	best, volume := "", ""
	for _, m := range objects(c, "volumeMounts") {
		mp := strings.TrimSuffix(str(m, "mountPath"), "/")
		if path == mp || strings.HasPrefix(path, mp+"/") {
			if len(mp) > len(best) {
				best, volume = mp, str(m, "name")
			}
		}
	}
	return best, volume
}

func volumeNamed(spec kobj, name string) kobj {
	for _, v := range objects(spec, "volumes") {
		if str(v, "name") == name {
			return v
		}
	}
	return nil
}

func hasObject(docs []kobj, kind, name string) bool {
	for _, d := range docs {
		if kindOf(d) == kind && nameOf(d) == name {
			return true
		}
	}
	return false
}

// ---- hm2: Release by commit

const (
	fakeGitLab      = "gitlab.tester.invalid"
	fakeProjectPath = "team/framewise"
	deployToken     = "tester-deploy-token"
	jobToken        = "tester-job-token"
)

func testReleaseByCommit(harness *test_case_harness.TestCaseHarness) error {
	for tool, why := range map[string]string{"git": "it runs your deploy job against a scratch repository", "helm": "it renders your chart before and after the release"} {
		if _, err := needTool(tool, why); err != nil {
			return err
		}
	}
	dir := repoDir(harness)
	cfg, err := loadCI(dir)
	if err != nil {
		return err
	}
	if err := chartFiles(dir); err != nil {
		return err
	}
	test, build, deploy := cfg.stageIndex("test"), cfg.stageIndex("build"), cfg.stageIndex("deploy")
	if build < 0 || deploy < 0 || !(test < build && build < deploy) {
		return fmt.Errorf("expected stages: to have test, build and deploy in that order, got %v", cfg.stages)
	}
	builds, deploys := cfg.stageJobs("build"), cfg.stageJobs("deploy")
	if len(builds) != 1 || len(deploys) != 1 {
		return fmt.Errorf("expected one job in the build stage and one in the deploy stage, got %d and %d", len(builds), len(deploys))
	}
	b, d := builds[0], deploys[0]
	for _, j := range []*ciJob{b, d} {
		if !onlyOnDefaultBranch(j) {
			return fmt.Errorf("expected job %q to run only on the default branch (rules: - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH): a merge request must not release", j.name)
		}
	}
	if !containsAny(strings.Join(b.script, "\n"), "CI_COMMIT_SHORT_SHA", "CI_COMMIT_SHA") {
		return fmt.Errorf("expected job %q to tag the image with the commit (CI_COMMIT_SHORT_SHA), so every release is one commit", b.name)
	}
	if !containsAny(strings.Join(d.needs, " "), b.name) {
		return fmt.Errorf("expected job %q to have needs: [%s]: no release without an image", d.name, b.name)
	}
	harness.Logger.Successf("✓ %s: %s builds on the default branch, %s releases after it", ciFile, b.name, d.name)

	app, err := readYAMLFile(filepath.Join(dir, "deploy", "argocd-application.yaml"))
	if err != nil {
		return fmt.Errorf("expected deploy/argocd-application.yaml, the one kobj to hand devops: %v", err)
	}
	switch {
	case kindOf(app) != "Application":
		return fmt.Errorf("expected deploy/argocd-application.yaml to be an Argo CD Application, got kind %q", kindOf(app))
	case strings.Trim(str(app, "spec", "source", "path"), "/") != chartDir:
		return fmt.Errorf("expected the Application's spec.source.path to be %s, got %q", chartDir, str(app, "spec", "source", "path"))
	case !containsAny(fmt.Sprint(at(app, "spec", "source", "helm", "valueFiles")), "values-nonprod.yaml"):
		return fmt.Errorf("expected the Application's spec.source.helm.valueFiles to list values-nonprod.yaml")
	case at(app, "spec", "syncPolicy", "automated", "selfHeal") != true:
		return fmt.Errorf("expected the Application to sync automatically with selfHeal: true, so a hand edit in the cluster is undone")
	}
	harness.Logger.Successf("✓ the Argo CD Application syncs %s with values-nonprod.yaml and heals drift", chartDir)

	docs, _, err := helmTemplate(dir)
	if err != nil {
		return err
	}
	migrateD, _ := findContainer(docs, "migrate")
	if migrateD == nil || str(migrateD, "metadata", "annotations", "argocd.argoproj.io/hook") != "PreSync" {
		return fmt.Errorf("expected the migrate Job to carry the annotation argocd.argoproj.io/hook: PreSync, so the schema changes before new pods start")
	}
	harness.Logger.Successf("✓ migrations run as a PreSync hook")
	oldTag := imageTag(firstImage(docs))

	// A scratch GitLab: a bare repository that the job's push URLs are rewritten to.
	tmp := tempDir(harness)
	origin := filepath.Join(tmp, "origin.git")
	seed := filepath.Join(tmp, "seed")
	if _, err := git(tmp, "init", "-q", "--bare", "-b", "main", origin); err != nil {
		return err
	}
	if err := copyRepo(dir, seed); err != nil {
		return err
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "The merge being released"}, {"push", "-q", origin, "main"}} {
		if _, err := git(seed, args...); err != nil {
			return err
		}
	}
	sha, err := git(seed, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	job := filepath.Join(tmp, "job")
	remote := fmt.Sprintf("https://gitlab-ci-token:%s@%s/%s.git", jobToken, fakeGitLab, fakeProjectPath)
	for _, args := range [][]string{{"clone", "-q", origin, job}, {"-C", job, "checkout", "-q", "--detach", sha}, {"-C", job, "remote", "set-url", "origin", remote}} {
		if _, err := git(tmp, args...); err != nil {
			return err
		}
	}

	pre := map[string]string{
		"CI": "true", "GITLAB_CI": "true", "CI_PROJECT_DIR": job, "CI_JOB_NAME": d.name, "CI_JOB_STAGE": "deploy",
		"CI_COMMIT_SHA": sha, "CI_COMMIT_SHORT_SHA": sha[:8], "CI_COMMIT_BRANCH": "main", "CI_COMMIT_REF_NAME": "main",
		"CI_DEFAULT_BRANCH": "main", "CI_PIPELINE_SOURCE": "push", "CI_SERVER_HOST": fakeGitLab,
		"CI_SERVER_URL": "https://" + fakeGitLab, "CI_PROJECT_PATH": fakeProjectPath, "CI_PROJECT_NAME": "framewise",
		"CI_REPOSITORY_URL": remote, "CI_JOB_TOKEN": jobToken, "DEPLOY_TOKEN": deployToken,
	}
	vars := cfg.jobVariables(d, pre)
	env := gitEnv()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	env = append(env, gitRewrites(origin)...)
	var script strings.Builder
	script.WriteString("set -e\n")
	for _, line := range append(append([]string{}, d.beforeScript...), d.script...) {
		fmt.Fprintf(&script, "printf '%%s\\n' %s\n%s\n", shellQuote("$ "+line), line)
	}
	harness.Logger.Infof("Running job %q's script with sh on this machine, in a clone of a scratch repository (CI_COMMIT_SHORT_SHA=%s)", d.name, sha[:8])
	out, err := command(job, env, 5*time.Minute, "sh", "-c", script.String())
	if err != nil {
		return fmt.Errorf("expected job %q to succeed, it failed (%v):\n%s\nThe tester answers pushes to https://<user>:${DEPLOY_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git and to origin", d.name, err, indent(tail(out, 25)))
	}
	harness.Logger.Infof("%s", indent(tail(out, 10)))

	commits, err := git(origin, "rev-list", sha+"..main")
	if err != nil {
		return err
	}
	if n := len(lines(commits)); n != 1 {
		return fmt.Errorf("expected job %q to push exactly one commit to main, it pushed %d", d.name, n)
	}
	message, _ := git(origin, "log", "-1", "--format=%B", "main")
	if !containsAny(message, "[skip ci]", "[ci skip]") {
		return fmt.Errorf("expected the release commit's message to contain [skip ci], or it starts another pipeline that releases again. Got %q", strings.TrimSpace(message))
	}
	changed, _ := git(origin, "diff", "--name-only", sha, "main")
	if changed != chartDir+"/values-nonprod.yaml" {
		return fmt.Errorf("expected the release commit to change only %s/values-nonprod.yaml, it changed: %s", chartDir, strings.Join(lines(changed), ", "))
	}
	harness.Logger.Successf("✓ one commit to main, only values-nonprod.yaml, with [skip ci]: %q", strings.TrimSpace(strings.Split(message, "\n")[0]))

	after := filepath.Join(tmp, "after")
	if _, err := git(tmp, "clone", "-q", origin, after); err != nil {
		return err
	}
	docs, _, err = helmTemplate(after)
	if err != nil {
		return err
	}
	for where, image := range appImages(docs) {
		if imageTag(image) != sha[:8] {
			return fmt.Errorf("expected %s to run image tag %s after the release, got %q", where, sha[:8], image)
		}
	}
	harness.Logger.Successf("✓ after the release the chart renders image tag %s", sha[:8])
	if _, err := git(after, "revert", "--no-edit", "HEAD"); err != nil {
		return err
	}
	docs, _, err = helmTemplate(after)
	if err != nil {
		return err
	}
	if got := imageTag(firstImage(docs)); got != oldTag {
		return fmt.Errorf("expected git revert of the release to bring back image tag %s, got %s", oldTag, got)
	}
	harness.Logger.Successf("✓ git revert brings back image tag %s", oldTag)
	return nil
}

// firstImage is the image of the container that runs serve.
func firstImage(docs []kobj) string {
	_, c := findContainer(docs, "serve")
	return str(c, "image")
}

// gitRewrites point the URLs a deploy job may push to at the scratch repository.
func gitRewrites(origin string) []string {
	base := fakeGitLab + "/" + fakeProjectPath
	prefixes := []string{"https://" + base, "http://" + base}
	for _, user := range []string{"ci", "gitlab-ci-token", "oauth2", "deploy", "deploy-token", "framewise-ci", "git", "token", "project_bot"} {
		prefixes = append(prefixes, "https://"+user+"@"+base)
		for _, token := range []string{deployToken, jobToken} {
			prefixes = append(prefixes, "https://"+user+":"+token+"@"+base)
		}
	}
	env := []string{fmt.Sprintf("GIT_CONFIG_COUNT=%d", 2*len(prefixes))}
	for i, p := range prefixes {
		for j, suffix := range []string{".git", ""} {
			n := 2*i + j
			env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=url.%s.insteadOf", n, origin), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s%s", n, p, suffix))
		}
	}
	return env
}
