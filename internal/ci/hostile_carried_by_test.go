package ci

// TestHostileRunnerHonoursCarriedBy pins the C++ hostile-values runner's read
// of the manifest's port-exemption marks (testdata/conformance/tables/
// MANIFEST.txt; the Go harness's ReadManifest is the oracle for the form):
// `carried-by=<ports>` and `uncarried=<verdict>` after a json-hostile verdict.
// A row whose carried-by list does not name cpp, or whose uncarried value
// names cpp, is a SKIP printed by name and never a failure. An unknown key is
// a runner error, never a spelling silently dropped.
//
// It compiles test/tables/hostile_main.cpp with -DHOSTILE_PARSE_ONLY: the same
// row reader without the generated tables or the codec checks, so this test is
// a function of the source and a scratch manifest, not of the C++ corpus.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildHostileRowReader(t *testing.T) string {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skipf("the C++ hostile-row reader needs a compiler: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "hostile_row_reader")
	cmd := exec.Command(cxx, "-std=c++17", "-Wall", "-Wextra", "-Werror", "-Wshadow",
		"-DHOSTILE_PARSE_ONLY", filepath.Join(repoRoot(t), "test", "tables", "hostile_main.cpp"),
		"-o", binary)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s did not compile the hostile row reader: %v\n%s", cxx, err, out)
	}
	return binary
}

func runHostileRowReader(t *testing.T, binary, manifest string) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "MANIFEST.txt")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(binary, path).CombinedOutput()
	return string(out), err
}

func TestHostileRunnerHonoursCarriedBy(t *testing.T) {
	t.Parallel()
	binary := buildHostileRowReader(t)

	// The plant is carried-by=go,cpp: a reader that only recognises the exact
	// single-port spelling `cpp` skips it, and this test goes red on the SKIP.
	const manifest = `# the shapes the conformance manifest allows
json-hostile plain         tabledemo RootConfig /tree 0,0,0,0,0,false,read
json-hostile goonly        tabledemo RootConfig /tree refused carried-by=go uncarried=0,0,0,1,0,false,read
json-hostile cpnonly       tabledemo RootConfig /tree refused carried-by=cpp uncarried=0,0,0,0,0,false,read
json-hostile mixed         tabledemo RootConfig /tree refused carried-by=go,cpp uncarried=0,0,0,1,0,false,read
json-hostile uncarried-cpp tabledemo RootConfig /tree refused carried-by=go uncarried=cpp
json-hostile both-named   tabledemo RootConfig /tree refused carried-by=cpp uncarried=cpp
json-hostile lookalike     tabledemo RootConfig /tree refused carried-by=cpp2
json-hostile uncarried-refused tabledemo RootConfig /tree refused carried-by=go uncarried=refused
`
	out, err := runHostileRowReader(t, binary, manifest)
	if err != nil {
		t.Fatalf("the unmarked/marked manifest was refused: %v\n%s", err, out)
	}
	for _, want := range []string{
		"CHECK plain\n",
		"SKIP goonly (not carried by cpp)\n",
		"CHECK cpnonly\n",
		"CHECK mixed\n", // the plant: cpp is named in a list of more than one port
		"SKIP uncarried-cpp (not carried by cpp)\n",
		"SKIP both-named (not carried by cpp)\n", // uncarried names cpp even though carried-by does too
		"SKIP lookalike (not carried by cpp)\n",
		"SKIP uncarried-refused (not carried by cpp)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("manifest row decision missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SKIP mixed") {
		t.Errorf("carried-by=go,cpp names cpp, but the reader skipped it:\n%s", out)
	}
}

func TestHostileRunnerRejectsAnUnknownManifestKey(t *testing.T) {
	t.Parallel()
	binary := buildHostileRowReader(t)

	const manifest = `json-hostile planted tabledemo RootConfig /tree refused carried-by=go uncarried=0,0,0,1,0,false,read bogus=1
`
	out, err := runHostileRowReader(t, binary, manifest)
	if err == nil {
		t.Fatalf("a row with an unknown key passed; the runner must error:\n%s", out)
	}
	if !strings.Contains(out, "ERROR planted: unknown manifest key bogus=1") {
		t.Errorf("the runner error does not name the unknown key:\n%s", out)
	}
}

func TestHostileRunnerRejectsMalformedMarks(t *testing.T) {
	t.Parallel()
	binary := buildHostileRowReader(t)

	// The oracle (ReadManifest) takes six fields or eight: the marks are the
	// PAIR carried-by=<ports> uncarried=<verdict>. A lone half, an empty port
	// or a value that is not a verdict is a runner error, never a silent skip.
	for _, tc := range []struct {
		name     string
		row      string
		wantText string
	}{
		{"empty-port", "json-hostile ports tabledemo RootConfig /tree refused carried-by=go,,cpp uncarried=refused\n", "carried-by names an empty port"},
		{"unknown-first", "json-hostile first tabledemo RootConfig /tree refused bogus=1 uncarried=refused\n", "unknown manifest key bogus=1"},
		{"unknown-lone", "json-hostile lone tabledemo RootConfig /tree refused bogus=1\n", "unknown manifest key bogus=1"},
		{"unknown-second", "json-hostile second tabledemo RootConfig /tree refused carried-by=go bogus=1\n", "unknown manifest key bogus=1"},
		{"too-many", "json-hostile many tabledemo RootConfig /tree refused carried-by=go uncarried=refused extra=1\n", "unknown manifest key extra=1"},
	} {
		out, err := runHostileRowReader(t, binary, tc.row)
		if err == nil {
			t.Errorf("%s: malformed mark passed the runner:\n%s", tc.name, out)
			continue
		}
		if !strings.Contains(out, tc.wantText) {
			t.Errorf("%s: runner error does not say %q:\n%s", tc.name, tc.wantText, out)
		}
	}
}

func TestHostileRunnerRejectsADuplicateOrEmptyMark(t *testing.T) {
	t.Parallel()
	binary := buildHostileRowReader(t)

	for _, tc := range []struct {
		name     string
		row      string
		wantText string
	}{
		{"duplicate", "json-hostile dup tabledemo RootConfig /tree refused carried-by=go carried-by=cpp\n", "duplicate manifest key carried-by=cpp"},
		{"empty", "json-hostile empty tabledemo RootConfig /tree refused carried-by= uncarried=refused\n", "manifest key carried-by= names no value"},
	} {
		out, err := runHostileRowReader(t, binary, tc.row)
		if err == nil {
			t.Errorf("%s: malformed mark passed the runner:\n%s", tc.name, out)
			continue
		}
		if !strings.Contains(out, tc.wantText) {
			t.Errorf("%s: runner error does not say %q:\n%s", tc.name, tc.wantText, out)
		}
	}
}
