package internal

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadCI(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ciFile), []byte(`
default:
  image: ${CI_IMAGE}
  before_script: [uv sync --locked]
variables:
  UV_CACHE_DIR: .uv-cache
cache: {key: {files: [uv.lock]}, paths: [.uv-cache]}
stages: [lint, test, build]
.common: &common
  - echo common
lint:
  stage: lint
  script: [*common, uv run ruff check .]
test:
  services: [{name: "${POSTGRES_IMAGE}", alias: db}]
  script: [uv run pytest]
build:
  stage: build
  image: buildah
  before_script: []
  needs: [test]
  rules: [{if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH}]
  script: [buildah bud -t x:$CI_COMMIT_SHORT_SHA .]
`), 0o644)
	cfg, err := loadCI(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.order, []string{"lint", "test", "build"}) {
		t.Errorf("order: %v", cfg.order)
	}
	lint := cfg.jobs["lint"]
	if !reflect.DeepEqual(lint.script, []string{"echo common", "uv run ruff check ."}) || lint.beforeScript[0] != "uv sync --locked" {
		t.Errorf("lint: %+v", lint)
	}
	test := cfg.jobs["test"]
	if test.stage != "test" || test.services[0].alias != "db" || test.cachePaths[0] != ".uv-cache" {
		t.Errorf("test: %+v", test)
	}
	vars := cfg.jobVariables(test, nil)
	if got := expand(test.image, vars); got != ciImage() {
		t.Errorf("image: %q", got)
	}
	build := cfg.jobs["build"]
	if len(build.beforeScript) != 0 || build.needs[0] != "test" || !onlyOnDefaultBranch(build) || onlyOnDefaultBranch(test) {
		t.Errorf("build: %+v", build)
	}
}

func TestLoadCIRejectsAMapInAScript(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ciFile), []byte("job:\n  script:\n    - sed -i \"s/a: b/\" f\n"), 0o644)
	if _, err := loadCI(dir); err == nil || !strings.Contains(err.Error(), "quote the whole line") {
		t.Errorf("expected an error about quoting, got %v", err)
	}
}

func TestServiceAliases(t *testing.T) {
	got := serviceAliases("quay.example.internal/mirror/postgres:16", "")
	want := []string{"quay.example.internal__mirror__postgres", "quay.example.internal-mirror-postgres", "postgres"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestGitRewritesCoverTheDeployTokenURL(t *testing.T) {
	env := strings.Join(gitRewrites("/tmp/origin.git"), "\n")
	for _, url := range []string{
		"https://ci:" + deployToken + "@" + fakeGitLab + "/" + fakeProjectPath + ".git",
		"https://gitlab-ci-token:" + jobToken + "@" + fakeGitLab + "/" + fakeProjectPath + ".git",
	} {
		if !strings.Contains(env, "="+url+"\n") && !strings.HasSuffix(env, "="+url) {
			t.Errorf("no rewrite for %s", url)
		}
	}
}
