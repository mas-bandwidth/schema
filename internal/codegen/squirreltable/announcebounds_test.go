package squirreltable_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestSquirrelAnnounceHonorsReceiverBounds holds the receiver's MaxBytes and
// MaxEntries bounds on AnnounceRead to the Go engine's VERDICT (docs/rulings.json
// refusal-verdict-only): refused under either bound, accepted with none. The
// reason each names is per-language detail, printed and never compared.
func TestSquirrelAnnounceHonorsReceiverBounds(t *testing.T) {
	t.Parallel()
	squirrelAnnounceBounds(t, nil)
}

// TestSquirrelAnnounceBoundsReasonIsDiagnostic renames the Squirrel
// runtime's vocabulary_too_large reason: the verdicts must not move.
func TestSquirrelAnnounceBoundsReasonIsDiagnostic(t *testing.T) {
	t.Parallel()
	const name = "schema_table_message.nut"
	body := string(squirreltable.Runtime()[name])
	const from = "\"vocabulary_too_large\""
	if strings.Count(body, from) == 0 {
		t.Fatalf("the reason plant finds no %s in %s", from, name)
	}
	squirrelAnnounceBounds(t, map[string][]byte{name: []byte(strings.ReplaceAll(body, from, "\"renamed_by_the_plant\""))})
}

func squirrelAnnounceBounds(t *testing.T, patch map[string][]byte) {
	t.Helper()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/examples"})
	ann := announcementWithEntries(u, tablewire.MessageVocabulary(u))

	type bounds struct{ maxBytes, maxEntries int }
	cases := []struct {
		name        string
		b           bounds
		wantRefused bool
	}{
		{"bytes", bounds{maxBytes: 8}, true},
		{"entries", bounds{maxEntries: 1}, true},
		{"none", bounds{}, false},
	}

	want := map[string]string{}
	for _, c := range cases {
		gv := &tablewire.Vocabulary{MaxBytes: c.b.maxBytes, MaxEntries: c.b.maxEntries}
		gerr := gv.AnnounceRead(ann, &tabletext.Report{})
		if gerr != nil && !tablewire.ReadRefusal(gerr) {
			t.Fatalf("case %s: the Go engine failed rather than refused: %v", c.name, gerr)
		}
		gotRefused := gerr != nil
		if gotRefused != c.wantRefused {
			t.Fatalf("case %s: Go engine refused=%v want %v (err=%v)", c.name, gotRefused, c.wantRefused, gerr)
		}
		want[c.name] = "accept"
		if gotRefused {
			want[c.name] = "refused"
		}
		t.Logf("case %s: Go engine %s %s", c.name, want[c.name], tablewire.RefusalReason(gerr))
	}

	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, patch) {
		if strings.Contains(name, "/") {
			continue
		}
		writeFile(t, filepath.Join(dir, name), b)
	}
	var script strings.Builder
	script.WriteString(`local I=dofile("inputs.nut",true); local T=dofile("schema_module.nut",true).require("` + squirreltable.ModuleName(u) + `");
function check(name){ local v={}; local ar={}; if(name=="bytes")v.MaxBytes<-8; if(name=="entries")v.MaxEntries<-1; local ok=T.AnnounceRead(v,I["ann"],ar); print(name+":"+(ok?"accept":"refused")+":"+(("reason" in ar)?ar.reason:"-")+"\n"); }
check("bytes"); check("entries"); check("none");
`)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"ann": ann})

	got := map[string]string{}
	why := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		p := strings.SplitN(line, ":", 3)
		if len(p) == 3 {
			got[p[0]] = p[1]
			why[p[0]] = p[2]
		}
	}
	for k, expected := range want {
		if got[k] != expected {
			t.Errorf("case %s: Squirrel %s (%s), want %s as the Go engine", k, got[k], why[k], expected)
		}
	}
}
