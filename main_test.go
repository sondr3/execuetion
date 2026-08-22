package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sondr3/execuetion/internal/cuemod"
	"github.com/sondr3/execuetion/internal/pin"
)

var update = flag.Bool("update", false, "update golden files")

// copyRepo copies a testdata repo into a temp dir so tests can write into it.
func copyRepo(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copying %s: %v", src, err)
	}
	return dst
}

func TestGenerateGolden(t *testing.T) {
	formats := []struct {
		format string
		ext    string
	}{
		{"kyaml", ".golden"},
		{"yaml", ".yaml.golden"},
	}
	for _, f := range formats {
		t.Run(f.format, func(t *testing.T) {
			workflows, err := generate("testdata/repo", f.format)
			if err != nil {
				t.Fatalf("generate returned error: %v", err)
			}
			if len(workflows) != 2 {
				t.Fatalf("expected 2 workflows, got %d", len(workflows))
			}

			for _, wf := range workflows {
				golden := strings.TrimSuffix(wf.Source, ".cue") + f.ext
				if *update {
					if err := os.WriteFile(golden, wf.Data, 0o644); err != nil {
						t.Fatalf("failed to update golden file: %v", err)
					}
				}
				expected, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf(
						"failed to read golden file %s (run with -update to create): %v",
						golden,
						err,
					)
				}
				if string(wf.Data) != string(expected) {
					t.Errorf(
						"output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s",
						wf.Source,
						wf.Data,
						expected,
					)
				}
			}
		})
	}
}

// fakeResolver implements pin.Resolver from a fixed map, so the pinned
// golden and verify tests never touch the network.
type fakeResolver map[string]pin.Resolution

func (f fakeResolver) Resolve(_ context.Context, owner, repo, ref string) (pin.Resolution, error) {
	res, ok := f[owner+"/"+repo+"@"+ref]
	if !ok {
		return pin.Resolution{}, fmt.Errorf("no fake pin for %s/%s@%s", owner, repo, ref)
	}
	return res, nil
}

const (
	fakeCheckoutSHA = "1111111111111111111111111111111111111111"
	fakeSetupGoSHA  = "2222222222222222222222222222222222222222"
)

// testResolver pins the two actions used by testdata/repo, with full
// versions so the goldens exercise the version-comment path.
func testResolver() fakeResolver {
	return fakeResolver{
		"actions/checkout@v7": {SHA: fakeCheckoutSHA, Version: "v7.7.1"},
		"actions/setup-go@v6": {SHA: fakeSetupGoSHA, Version: "v6.6.0"},
		// Seeded full-version keys are used by verification.
		"actions/checkout@v7.7.1": {SHA: fakeCheckoutSHA, Version: "v7.7.1"},
		"actions/setup-go@v6.6.0": {SHA: fakeSetupGoSHA, Version: "v6.6.0"},
	}
}

func testPinner(t *testing.T) *pin.Pinner {
	t.Helper()
	return &pin.Pinner{
		Resolver: testResolver(),
		Cache:    pin.Open(filepath.Join(t.TempDir(), "pins.json")),
	}
}

func TestGeneratePinnedGolden(t *testing.T) {
	formats := []struct {
		format string
		ext    string
	}{
		{"kyaml", ".pinned.golden"},
		{"yaml", ".pinned.yaml.golden"},
	}
	for _, f := range formats {
		t.Run(f.format, func(t *testing.T) {
			workflows, err := generate("testdata/repo", f.format)
			if err != nil {
				t.Fatalf("generate returned error: %v", err)
			}
			if err := pinWorkflows(context.Background(), testPinner(t), workflows); err != nil {
				t.Fatalf("pinWorkflows returned error: %v", err)
			}

			for _, wf := range workflows {
				golden := strings.TrimSuffix(wf.Source, ".cue") + f.ext
				if *update {
					if err := os.WriteFile(golden, wf.Data, 0o644); err != nil {
						t.Fatalf("failed to update golden file: %v", err)
					}
				}
				expected, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf(
						"failed to read golden file %s (run with -update to create): %v",
						golden,
						err,
					)
				}
				if string(wf.Data) != string(expected) {
					t.Errorf(
						"output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s",
						wf.Source,
						wf.Data,
						expected,
					)
				}
			}
		})
	}
}

// pinnedRepo generates, pins, and writes testdata/repo into a temp root.
func pinnedRepo(t *testing.T) string {
	t.Helper()
	root := copyRepo(t, "testdata/repo")
	workflows, err := generate(root, "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if err := pinWorkflows(context.Background(), testPinner(t), workflows); err != nil {
		t.Fatalf("pinWorkflows returned error: %v", err)
	}
	if err := writeWorkflows(workflows); err != nil {
		t.Fatalf("writeWorkflows returned error: %v", err)
	}
	return root
}

func TestVerifyPins(t *testing.T) {
	root := pinnedRepo(t)

	problems, err := verifyPins(context.Background(), testPinner(t), root)
	if err != nil {
		t.Fatalf("verifyPins returned error: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected clean verify, got %v", problems)
	}

	// Tamper with a pinned SHA; the version comment now lies.
	ciYml := filepath.Join(root, workflowDir, "ci.yml")
	content, err := os.ReadFile(ciYml)
	if err != nil {
		t.Fatal(err)
	}
	evil := strings.Repeat("e", 40)
	tampered := strings.Replace(string(content), fakeCheckoutSHA, evil, 1)
	if err = os.WriteFile(ciYml, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	problems, err = verifyPins(context.Background(), testPinner(t), root)
	if err != nil {
		t.Fatalf("verifyPins returned error: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("expected 1 verify problem, got %v", problems)
	}
	if !strings.Contains(problems[0], "ci.yml") ||
		!strings.Contains(problems[0], "actions/checkout@v7.7.1") {
		t.Errorf("problem should name file and ref: %s", problems[0])
	}
}

// Verification also covers hand-written workflows, which check's byte
// comparison never sees.
func TestVerifyPinsHandwritten(t *testing.T) {
	root := pinnedRepo(t)
	handwritten := fmt.Sprintf(
		"name: manual\non: push\njobs:\n  x:\n    steps:\n      - uses: actions/checkout@%s # v7.7.1\n",
		strings.Repeat("e", 40),
	)
	if err := os.WriteFile(
		filepath.Join(root, workflowDir, "manual.yaml"),
		[]byte(handwritten),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	problems, err := verifyPins(context.Background(), testPinner(t), root)
	if err != nil {
		t.Fatalf("verifyPins returned error: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "manual.yaml") {
		t.Fatalf("expected 1 problem naming manual.yaml, got %v", problems)
	}
}

func TestVerifyPinsUnknownVersion(t *testing.T) {
	root := pinnedRepo(t)
	handwritten := fmt.Sprintf(
		"name: manual\non: push\njobs:\n  x:\n    steps:\n      - uses: ghost/missing@%s # v9.9.9\n",
		strings.Repeat("a", 40),
	)
	if err := os.WriteFile(
		filepath.Join(root, workflowDir, "manual.yaml"),
		[]byte(handwritten),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, err := verifyPins(context.Background(), testPinner(t), root)
	if err == nil || !strings.Contains(err.Error(), "ghost/missing@v9.9.9") {
		t.Fatalf("expected error naming the unresolvable ref, got %v", err)
	}
}

func TestFirstDiff(t *testing.T) {
	got := firstDiff([]byte("a\nb\nc"), []byte("a\nX\nc"))
	if !strings.Contains(got, "line 2") || !strings.Contains(got, `"b"`) ||
		!strings.Contains(got, `"X"`) {
		t.Errorf("firstDiff = %q", got)
	}
}

func TestGenerateInvalidFormat(t *testing.T) {
	_, err := generate("testdata/repo", "json")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid format") {
		t.Errorf("error does not mention invalid format: %v", err)
	}
}

func TestGenerateKyamlNoTabs(t *testing.T) {
	workflows, err := generate("testdata/repo", "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	for _, wf := range workflows {
		if strings.Contains(string(wf.Data), "\t") {
			t.Errorf("%s contains tabs", wf.Output)
		}
	}
}

func TestGenerateHeader(t *testing.T) {
	workflows, err := generate("testdata/repo", "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	for _, wf := range workflows {
		if !strings.HasPrefix(string(wf.Data), generatedMarker+"\n") {
			t.Errorf("%s: missing generated header", wf.Output)
		}
	}
}

func TestGenerateNotConcrete(t *testing.T) {
	_, err := generate("testdata/broken", "kyaml")
	if err == nil {
		t.Fatal("expected error for non-concrete workflow")
	}
	if !strings.Contains(err.Error(), "bad.cue") {
		t.Errorf("error does not mention the failing file: %v", err)
	}
}

func TestGenerateRejectsOnDiskCueMod(t *testing.T) {
	_, err := generate("testdata/cuemodrepo", "kyaml")
	if err == nil {
		t.Fatal("expected error when repo has a real cue.mod")
	}
	if !strings.Contains(err.Error(), "cue.mod") {
		t.Errorf("error does not mention cue.mod: %v", err)
	}
}

func TestGenerateNoWorkflows(t *testing.T) {
	_, err := generate(t.TempDir(), "kyaml")
	if err == nil {
		t.Fatal("expected error for repo without workflow sources")
	}
}

func TestInitCueMod(t *testing.T) {
	root := copyRepo(t, "testdata/repo")
	moduleCue := filepath.Join(root, workflowDir, "cue.mod", "module.cue")
	if err := os.Remove(moduleCue); err != nil {
		t.Fatal(err)
	}

	if err := initCueMod(root); err != nil {
		t.Fatalf("initCueMod returned error: %v", err)
	}
	content, err := os.ReadFile(moduleCue)
	if err != nil {
		t.Fatalf("failed to read written module.cue: %v", err)
	}
	for _, want := range []string{`module: "execuetion.dev"`, "cue.dev/x/githubactions@v0", cuemod.GithubActionsVersion} {
		if !strings.Contains(string(content), want) {
			t.Errorf("module.cue missing %q:\n%s", want, content)
		}
	}

	// Idempotent: a second run leaves the identical file in place.
	if err := initCueMod(root); err != nil {
		t.Fatalf("second initCueMod returned error: %v", err)
	}

	// The editor-facing cue.mod must not interfere with generation.
	if _, err := generate(root, "kyaml"); err != nil {
		t.Fatalf("generate with editor cue.mod returned error: %v", err)
	}
}

func TestOutputName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ci.cue", "ci.yml"},
		{".github/workflows/release.cue", "release.yml"},
	}
	for _, tt := range tests {
		if got := outputName(tt.in); got != tt.want {
			t.Errorf("outputName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteWorkflows(t *testing.T) {
	root := copyRepo(t, "testdata/repo")
	workflows, err := generate(root, "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if err := writeWorkflows(workflows); err != nil {
		t.Fatalf("writeWorkflows returned error: %v", err)
	}

	for _, name := range []string{"ci.yml", "release.yml"} {
		content, err := os.ReadFile(filepath.Join(root, workflowDir, name))
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}
		if !strings.HasPrefix(string(content), generatedMarker) {
			t.Errorf("%s missing generated header", name)
		}
	}
}

func TestCheck(t *testing.T) {
	root := copyRepo(t, "testdata/repo")
	workflows, err := generate(root, "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}

	problems, err := check(root, workflows, false)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 2 || !strings.Contains(problems[0], "missing") {
		t.Fatalf("expected 2 missing files, got %v", problems)
	}

	if err = writeWorkflows(workflows); err != nil {
		t.Fatalf("writeWorkflows returned error: %v", err)
	}
	problems, err = check(root, workflows, false)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected no problems for up to date repo, got %v", problems)
	}

	ciYml := filepath.Join(root, workflowDir, "ci.yml")
	if err = os.WriteFile(ciYml, []byte(generatedMarker+"\nstale: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err = check(root, workflows, false)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "out of date") {
		t.Fatalf("expected one out of date problem, got %v", problems)
	}
}

func TestCheckNormalizesLineEndings(t *testing.T) {
	root := copyRepo(t, "testdata/repo")
	workflows, err := generate(root, "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if err = writeWorkflows(workflows); err != nil {
		t.Fatalf("writeWorkflows returned error: %v", err)
	}

	// Simulate a Windows autocrlf checkout with trailing whitespace.
	ciYml := filepath.Join(root, workflowDir, "ci.yml")
	content, err := os.ReadFile(ciYml)
	if err != nil {
		t.Fatal(err)
	}
	mangled := strings.ReplaceAll(string(content), "\n", " \r\n")
	if err = os.WriteFile(ciYml, []byte(mangled), 0o644); err != nil {
		t.Fatal(err)
	}

	problems, err := check(root, workflows, false)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected CRLF checkout to pass check, got %v", problems)
	}
}

func TestCheckOrphan(t *testing.T) {
	root := copyRepo(t, "testdata/repo")
	workflows, err := generate(root, "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if err = writeWorkflows(workflows); err != nil {
		t.Fatalf("writeWorkflows returned error: %v", err)
	}

	orphan := filepath.Join(root, workflowDir, "deleted-source.yml")
	if err = os.WriteFile(orphan, []byte(generatedMarker+"\nname: orphan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	problems, err := check(root, workflows, false)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "no matching .cue source") {
		t.Fatalf("expected one orphan problem, got %v", problems)
	}

	// Orphans fail even when hand-written files are allowed.
	problems, err = check(root, workflows, true)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("expected orphan to fail with --allow-handwritten, got %v", problems)
	}
}

func TestCheckHandwritten(t *testing.T) {
	root := copyRepo(t, "testdata/repo")
	workflows, err := generate(root, "kyaml")
	if err != nil {
		t.Fatalf("generate returned error: %v", err)
	}
	if err = writeWorkflows(workflows); err != nil {
		t.Fatalf("writeWorkflows returned error: %v", err)
	}

	handwritten := filepath.Join(root, workflowDir, "manual.yaml")
	if err = os.WriteFile(handwritten, []byte("name: manual\non: push\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	problems, err := check(root, workflows, false)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 1 || !strings.Contains(problems[0], "hand-written") {
		t.Fatalf("expected one hand-written problem, got %v", problems)
	}

	problems, err = check(root, workflows, true)
	if err != nil {
		t.Fatalf("check returned error: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected no problems with --allow-handwritten, got %v", problems)
	}
}

func TestNormalize(t *testing.T) {
	in := []byte("a: 1  \r\nb: 2\t\r\nc: 3\n")
	want := "a: 1\nb: 2\nc: 3\n"
	if got := string(normalize(in)); got != want {
		t.Errorf("normalize() = %q, want %q", got, want)
	}
}

func TestIsGenerated(t *testing.T) {
	if !isGenerated([]byte(generatedMarker + "\nname: x\n")) {
		t.Error("expected marker to be detected")
	}
	if !isGenerated([]byte(generatedMarker + "\r\nname: x\r\n")) {
		t.Error("expected marker to be detected with CRLF line endings")
	}
	if isGenerated([]byte("name: x\n")) {
		t.Error("expected plain yaml to not be detected as generated")
	}
}

func TestFindRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findRepoRoot(nested)
	if err != nil {
		t.Fatalf("findRepoRoot returned error: %v", err)
	}
	// Resolve symlinks: on macOS t.TempDir lives under /var -> /private/var.
	wantResolved, _ := filepath.EvalSymlinks(root)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("findRepoRoot = %q, want %q", got, root)
	}
}

func TestFindRepoRootNotFound(t *testing.T) {
	if _, err := findRepoRoot(t.TempDir()); err == nil {
		t.Fatal("expected error outside a git repository")
	}
}
