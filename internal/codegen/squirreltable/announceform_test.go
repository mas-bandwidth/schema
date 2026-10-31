package squirreltable_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// goAnnounceVerdict is the Go oracle for one announcement, the engine's
// verdict on the hostile-corpus row's input (docs/SPEC-TABLES.md §3.3).
func goAnnounceVerdict(data []byte) (malformed, refused bool, reason string) {
	var v tablewire.Vocabulary
	var report tabletext.Report
	err := v.AnnounceRead(data, &report)
	if refusal, ok := errors.AsType[*tablewire.MessageRefusal](err); ok {
		reason = refusal.Reason.String()
	}
	return report.Malformed, report.Refused, reason
}

// TestSquirrelAnnounceReadsFormBeforeLength pins the announcement reader to the
// Go engine's order: the form byte is read before the length
// (docs/SPEC-TABLES.md §3.3). The hostile-corpus row announce-newer-form
// (unit tables/examples, table PackConfig, input the single byte ff) expects
// newer_form because the form byte is read first. The test rebuilds that input
// and asks tablewire.Vocabulary.AnnounceRead for the verdict, then requires
// Squirrel's AnnounceRead to agree: a form byte this reader does not carry is
// newer_form even when the buffer is too short to hold the rest of an
// announcement; a form-2 byte is message_form_as_file; an empty buffer stays
// malformed.
func TestSquirrelAnnounceReadsFormBeforeLength(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnit(t, root, []string{"tables/examples/Pack.schema"})
	dir := t.TempDir()
	for name, b := range squirrelMessageFiles(t, u, nil) {
		if !strings.Contains(name, "/") {
			writeFile(t, filepath.Join(dir, name), b)
		}
	}
	module := squirreltable.ModuleName(u)
	inputs := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"ff", []byte{0xff}},
		{"ff00", []byte{0xff, 0x00}},
		{"two", []byte{0x02}},
	}
	var script strings.Builder
	fmt.Fprintf(&script, "local I=dofile(\"inputs.nut\",true);local T=dofile(\"schema_module.nut\",true).require(%q);\n", module)
	for _, in := range inputs {
		fmt.Fprintf(&script, "{local V={};local ar={};T.AnnounceRead(V,I[%q],ar);print(%q+\"|\"+(ar.malformed?\"malformed\":\"\")+\"|\"+(ar.refused?\"refused\":\"\")+\"|\"+((ar.reason==null)?\"\":ar.reason)+\"\\n\");}\n", in.name, in.name)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(script.String()))
	blobs := map[string][]byte{}
	for _, in := range inputs {
		blobs[in.name] = in.data
	}
	out := string(sqrun.Run(t, dir, "main.nut", blobs))
	lines := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if name, rest, ok := strings.Cut(line, "|"); ok {
			lines[name] = rest
		}
	}
	for _, in := range inputs {
		line, ok := lines[in.name]
		if !ok {
			t.Errorf("%s missing from probe: %s", in.name, out)
			continue
		}
		fields := strings.SplitN(line, "|", 3)
		gotMalformed := fields[0] == "malformed"
		gotRefused := fields[1] == "refused"
		gotReason := fields[2]
		wantMalformed, wantRefused, wantReason := goAnnounceVerdict(in.data)
		if gotMalformed != wantMalformed || gotRefused != wantRefused || gotReason != wantReason {
			t.Errorf("%s: Squirrel malformed=%v refused=%v reason=%q, Go oracle malformed=%v refused=%v reason=%q", in.name, gotMalformed, gotRefused, gotReason, wantMalformed, wantRefused, wantReason)
		}
	}
}
