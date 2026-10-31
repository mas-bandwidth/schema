package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCheckUnitsReadsTheCheckRecipe pins where the corpus comes from: the
// `./bin/schema check` lines of the `check:` recipe, and nothing after it.
func TestCheckUnitsReadsTheCheckRecipe(t *testing.T) {
	t.Parallel()
	mk := "all: bin/schema\n\t./bin/schema check not/this\n\ncheck: bin/schema docs-check\n" +
		"\t./bin/schema check examples\n\t./bin/schema check test/tables/V1.schema\n" +
		"\t./bin/schema check bench/A.schema bench/B.schema\n\techo done\n\n" +
		"other:\n\t./bin/schema check nor/this\n"
	got, err := CheckUnits(mk)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"examples"}, {"test/tables/V1.schema"}, {"bench/A.schema", "bench/B.schema"}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("CheckUnits = %q; want %q", got, want)
	}
	if _, err := CheckUnits("check:\n\techo nothing\n"); err == nil {
		t.Error("CheckUnits over an empty check recipe returned no error: an empty corpus would compare nothing and pass")
	}
}

// TestCheckUnitsReadsThisRepository holds the parse to the repository's own
// Makefile, so a reshaped check recipe fails here rather than in a release.
func TestCheckUnitsReadsThisRepository(t *testing.T) {
	t.Parallel()
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	units, err := CheckUnits(string(mk))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(units, func(u []string) bool { return slices.Equal(u, []string{"examples"}) }) {
		t.Errorf("the corpus read from the Makefile does not hold examples: %q", units)
	}
	for _, u := range units {
		for _, p := range u {
			if _, err := os.Stat(filepath.Join("../..", p)); err != nil {
				t.Errorf("corpus unit path %s: %v", p, err)
			}
		}
	}
}

func TestParseTargets(t *testing.T) {
	t.Parallel()
	help := "Usage of generate:\n  -lang string\n    \ttarget language (c, cpp, cs, go) (default \"cpp\")\n"
	got, err := ParseTargets(help)
	if err != nil || !slices.Equal(got, []string{"c", "cpp", "cs", "go"}) {
		t.Errorf("ParseTargets = %q, %v", got, err)
	}
	if _, err := ParseTargets("no list here"); err == nil {
		t.Error("ParseTargets found targets in text that names none")
	}
}

func TestFirstDifference(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"", "", -1},
		{"abc", "abc", -1},
		{"abc", "abd", 2},
		{"xbc", "abc", 0},
		{"ab", "abc", 2},
		{"abc", "ab", 2},
	} {
		if got := FirstDifference([]byte(c.a), []byte(c.b)); got != c.want {
			t.Errorf("FirstDifference(%q, %q) = %d; want %d", c.a, c.b, got, c.want)
		}
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCompareTreesBothWays pins the comparison: same listing and same bytes
// is nil; a byte, a missing file and an extra file each name the first
// difference in path order, with the byte, its line and the count.
func TestCompareTreesBothWays(t *testing.T) {
	t.Parallel()
	base := map[string]string{"A.h": "one\ntwo\n", "sub/B.h": "bee\n"}
	same := writeTree(t, base)
	if d, err := CompareTrees(same, writeTree(t, base)); err != nil || d != nil {
		t.Fatalf("identical trees: %v, %v", d, err)
	}

	d, err := CompareTrees(same, writeTree(t, map[string]string{"A.h": "one\ntwX\n", "sub/B.h": "bee\n"}))
	if err != nil || d == nil {
		t.Fatalf("one changed byte: %v, %v", d, err)
	}
	if d.File != "A.h" || d.Offset != 6 || d.Line != 2 || d.Old != 'o' || d.New != 'X' || d.Files != 1 {
		t.Errorf("one changed byte: %+v; want A.h byte 6 line 2 'o' -> 'X', 1 file", d)
	}
	if s := d.String(); !strings.Contains(s, "A.h first differs at byte 6 (line 2)") {
		t.Errorf("difference reads %q", s)
	}

	d, _ = CompareTrees(same, writeTree(t, map[string]string{"A.h": "one\ntwo\n"}))
	if d == nil || d.File != "sub/B.h" || d.Offset != -1 || d.New != -1 {
		t.Errorf("a file the new compiler stopped emitting: %+v", d)
	}
	d, _ = CompareTrees(same, writeTree(t, map[string]string{"A.h": "one\ntwo\n", "sub/B.h": "bee\n", "C.h": ""}))
	if d == nil || d.File != "C.h" || d.Old != -1 {
		t.Errorf("a file only the new compiler emits: %+v", d)
	}
	d, _ = CompareTrees(same, writeTree(t, map[string]string{"A.h": "one\ntwo\nthree\n", "sub/B.h": "BEE\n"}))
	if d == nil || d.File != "A.h" || d.Offset != 8 || d.Old != -1 || d.Files != 2 {
		t.Errorf("an appended tail and a second file: %+v", d)
	}
}

// fake is a compiler from a table: ids per unit, targets, and a generator per
// target that writes the given files, or refuses.
type fake struct {
	name    string
	targets []string
	ids     map[string]string // unit -> id; absent refuses the unit
	emit    map[string]string // "unit/lang" -> body of the one file; absent refuses
}

func (f fake) Name() string               { return f.name }
func (f fake) Targets() ([]string, error) { return f.targets, nil }
func (f fake) ID(u []string) (string, error) {
	if id, ok := f.ids[strings.Join(u, " ")]; ok {
		return id, nil
	}
	return "", errors.New("schema: 1 error(s)")
}
func (f fake) Generate(u []string, lang, out string) error {
	body, ok := f.emit[strings.Join(u, " ")+"/"+lang]
	if !ok {
		return errors.New("schema: target refuses the unit")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "Out.h"), []byte(body), 0o644)
}

// TestJudgeComparesOnlyUnderAnEqualId pins the decisions: an equal id compares
// every shared target and a byte refuses naming the unit, target, file and
// byte; a moved id, a unit one side refuses, a target one side lacks and a
// target one side refuses are listed and never refuse.
func TestJudgeComparesOnlyUnderAnEqualId(t *testing.T) {
	t.Parallel()
	units := [][]string{{"same"}, {"moved"}, {"newgrammar"}, {"refused"}}
	old := fake{
		name: "v1.0.0", targets: []string{"cpp", "go", "gone"},
		ids: map[string]string{"same": "0x1", "moved": "0x2", "refused": "0x4"},
		emit: map[string]string{
			"same/cpp": "x", "same/go": "y", "same/gone": "z",
			"moved/cpp": "a", "moved/go": "a",
			"refused/cpp": "r",
		},
	}
	nw := fake{
		name: "HEAD", targets: []string{"cpp", "go", "lua"},
		ids: map[string]string{"same": "0x1", "moved": "0x3", "newgrammar": "0x5", "refused": "0x4"},
		emit: map[string]string{
			"same/cpp": "x", "same/go": "y", "same/lua": "l",
			"moved/cpp": "b", "moved/go": "b",
			"refused/go": "g",
		},
	}
	r, err := Judge(old, nw, units, t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Differences) != 0 {
		t.Errorf("no byte differs under an equal id, and the gate refused: %q", r.Differences)
	}
	if len(r.Compared) != 2 || len(r.Moved) != 1 || len(r.Unreadable) != 1 {
		t.Errorf("compared %q, moved %q, unreadable %q; want 2, 1, 1", r.Compared, r.Moved, r.Unreadable)
	}
	if !slices.Contains(r.Moved, "moved: 0x2 -> 0x3") {
		t.Errorf("moved %q", r.Moved)
	}
	joined := strings.Join(r.Uncompared, "\n")
	for _, want := range []string{"target lua: v1.0.0 does not carry it", "target gone: HEAD does not carry it",
		"refused --lang cpp: HEAD refuses it", "refused --lang go: v1.0.0 refuses it"} {
		if !strings.Contains(joined, want) {
			t.Errorf("uncompared does not list %q:\n%s", want, joined)
		}
	}

	// THE REFUSAL: one byte under an equal id.
	nw.emit["same/go"] = "Y"
	r, err = Judge(old, nw, units, t.TempDir(), 3)
	if err != nil {
		t.Fatal(err)
	}
	want := "same --lang go: Out.h first differs at byte 0 (line 1): previous 0x79 'y', new 0x59 'Y' (1 file(s) differ)"
	if !slices.Equal(r.Differences, []string{want}) {
		t.Errorf("differences %q; want %q", r.Differences, want)
	}
	var out bytes.Buffer
	r.Print(&out, "v1.0.0", "HEAD")
	if !strings.Contains(out.String(), "DIFFERENCE under an equal protocol id: "+want) || !strings.Contains(out.String(), "REFUSED") {
		t.Errorf("printed report does not refuse by name:\n%s", out.String())
	}
}

func TestNewestRelease(t *testing.T) {
	t.Parallel()
	got, ok := NewestRelease([]string{"v2.5.0", "v2.10.0", "v2.9.9", "main-0123456789ab", "v3.0.0-rc1"})
	if !ok || got != "v2.10.0" {
		t.Errorf("NewestRelease = %q, %v; want v2.10.0", got, ok)
	}
	if _, ok := NewestRelease([]string{"main-0123456789ab"}); ok {
		t.Error("NewestRelease found a release among promotion tags only")
	}
}
