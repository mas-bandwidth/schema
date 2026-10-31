package squirreltable_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestSquirrelAnnounceHonorsReceiverBounds(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/examples"})
	ann := announcementWithEntries(u, ir.TableVocabulary(u))

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

	for _, c := range cases {
		gv := &tablewire.Vocabulary{MaxBytes: c.b.maxBytes, MaxEntries: c.b.maxEntries}
		gerr := gv.AnnounceRead(ann, &tabletext.Report{})
		var ref *tablewire.MessageRefusal
		gotRefused := errors.As(gerr, &ref) && ref.Reason == tablewire.ReasonVocabularyTooLarge
		if gotRefused != c.wantRefused {
			t.Fatalf("case %s: Go engine refused=%v want %v (err=%v)", c.name, gotRefused, c.wantRefused, gerr)
		}
	}

	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if strings.Contains(name, "/") {
			continue
		}
		writeFile(t, filepath.Join(dir, name), b)
	}
	var script strings.Builder
	script.WriteString(`local I=dofile("inputs.nut",true); local T=dofile("schema_module.nut",true).require("` + squirreltable.ModuleName(u) + `");
function check(name){ local v={}; local ar={}; if(name=="bytes")v.MaxBytes<-8; if(name=="entries")v.MaxEntries<-1; local ok=T.AnnounceRead(v,I["ann"],ar); print(name+":"+(ok?"accept":ar.reason)+"\n"); }
check("bytes"); check("entries"); check("none");
`)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"ann": ann})

	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		p := strings.SplitN(line, ":", 2)
		if len(p) == 2 {
			got[p[0]] = p[1]
		}
	}
	want := map[string]string{
		"bytes":   "vocabulary_too_large",
		"entries": "vocabulary_too_large",
		"none":    "accept",
	}
	for k, expected := range want {
		if got[k] != expected {
			t.Errorf("case %s: Squirrel %s, want %s", k, got[k], expected)
		}
	}
}
