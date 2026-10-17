package gotable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
)

// jsonTestSuiteProbe is the file runGeneratedEdited adds to the generated
// package: the emitted walk's JSON LAYER over one whole text
// (docs/SPEC-TABLES.md §16.11) — the leading byte-order mark, one value, then
// whitespace, then the end of the input — reached in-package, as the per-table
// FromJson reaches it.
const jsonTestSuiteProbe = `package probe

func jsonTestSuiteAccepts(text []byte) bool {
	var report TableReport
	in := tableJsonIn{text: text, report: &report}
	in.byteOrderMark()
	ok := in.skipValue(0)
	in.space()
	return ok && !in.bad && in.pos == len(in.text)
}
`

// jsonTestSuiteRun walks every corpus file and prints one verdict line each.
// Inside this generated process, and only here, it also holds the two bounds:
// each file's walk allocates nothing (§16.1), and its best of three runs is
// under 10 ms, the corpus under 2 s. The entry itself is pinned beside them:
// RootFromJson skips a leading byte-order mark and lands the same value.
const jsonTestSuiteRun = `package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const corpus = %q

func TestJSONTestSuiteProbe(t *testing.T) {
	entries, err := os.ReadDir(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var total time.Duration
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(corpus, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		verdict := "refuse"
		if jsonTestSuiteAccepts(data) {
			verdict = "accept"
		}
		if n := testing.AllocsPerRun(3, func() { jsonTestSuiteAccepts(data) }); n != 0 {
			t.Errorf("%%s: the walk allocated %%v times, want 0", e.Name(), n)
		}
		best := time.Duration(1 << 62)
		for i := 0; i < 3; i++ {
			start := time.Now()
			jsonTestSuiteAccepts(data)
			if d := time.Since(start); d < best {
				best = d
			}
		}
		if best > 10*time.Millisecond {
			t.Errorf("%%s: the walk took %%v at best of 3, want under 10ms", e.Name(), best)
		}
		total += best
		fmt.Printf("JTS\t%%s\t%%s\n", e.Name(), verdict)
	}
	if total > 2*time.Second {
		t.Errorf("the corpus took %%v, want under 2s", total)
	}

	var plain, marked Root
	var plainReport, markedReport TableReport
	if !RootFromJson(&plain, []byte("{\"n\":7}"), &plainReport) || !RootFromJson(&marked, []byte("\xEF\xBB\xBF{\"n\":7}"), &markedReport) ||
		marked != plain || markedReport != plainReport || marked.N != 7 {
		t.Errorf("a leading byte-order mark: %%+v %%+v, want %%+v %%+v", marked, markedReport, plain, plainReport)
	}
	for _, text := range []string{" \xEF\xBB\xBF{}", "{}\xEF\xBB\xBF", "\xEF\xBB{}", "\xEF\xBB\xBF\xEF\xBB\xBF{}", "\xFF\xFE{}"} {
		var v Root
		var r TableReport
		if RootFromJson(&v, []byte(text), &r) || !r.Malformed {
			t.Errorf("%%q: read clean, want Malformed", text)
		}
	}

	// the variable class reads through its builder, and skips the mark the same way
	for _, c := range []struct {
		text string
		ok   bool
	}{{"\xEF\xBB\xBF{\"n\":7}", true}, {"{\"n\":7}", true}, {" \xEF\xBB\xBF{\"n\":7}", false}, {"\xEF\xBB{\"n\":7}", false}} {
		var b DocBuilder
		b.Init()
		var r TableReport
		ok := DocFromJson(&b, []byte(c.text), &r)
		if ok != c.ok || (ok && (b.GetRoot().N != 7 || r != (TableReport{}))) || (!ok && !r.Malformed) {
			t.Errorf("variable %%q: ok %%v %%+v, want ok %%v", c.text, ok, r, c.ok)
		}
		b.Shutdown()
	}
}
`

// TestJSONTestSuiteGenerated holds the emitted Go walk to JSONTestSuite
// (docs/SPEC-TABLES.md §16.11), in a generated unit (SCHEMA_SLOW=1): every
// vendored file against the standard's table, PENDING's files allowed to
// disagree and logged, with the walk's memory and time bounds held inside the
// generated process.
func TestJSONTestSuiteGenerated(t *testing.T) {
	t.Parallel()
	s, err := jsontestsuite.Load("../../..")
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := filepath.Abs(filepath.Join("../../..", jsontestsuite.CorpusDir))
	if err != nil {
		t.Fatal(err)
	}
	out, err := runGeneratedEdited(t, "package probe\nfixed table Root { n int32 }\ntable Doc { n int32 }\n", fmt.Sprintf(jsonTestSuiteRun, corpus),
		func(files map[string][]byte) { files["jsontestsuite_probe.go"] = []byte(jsonTestSuiteProbe) },
		"-v", "-run", "^TestJSONTestSuiteProbe$")
	if err != nil {
		t.Fatalf("generated probe: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] != "JTS" {
			continue
		}
		if _, dup := got[fields[1]]; dup {
			t.Fatalf("%s: two verdicts from the probe", fields[1])
		}
		got[fields[1]] = fields[2] == "accept"
	}
	if len(got) != len(s.Files) {
		t.Fatalf("the probe gave %d verdicts for %d files\n%s", len(got), len(s.Files), out)
	}
	mismatches, pending := jsontestsuite.Check(s, got)
	for _, p := range pending {
		t.Logf("pending: %s", p)
	}
	for _, m := range mismatches {
		t.Errorf("mismatch: %s", m)
	}
}
