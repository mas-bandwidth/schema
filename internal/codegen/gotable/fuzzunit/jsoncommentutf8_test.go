package tabledemo_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	tabledemo "github.com/mas-bandwidth/schema/v2/internal/codegen/gotable/fuzzunit"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestGeneratedJSONCommentIsWellFormedUTF8 holds the generated reader to the
// comment-text rule in docs/SPEC-TABLES.md §16.2, with tabletext as reference.
func TestGeneratedJSONCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	const corpus = "../../../../testdata/wire/json-comment-text-rules/"
	manifest, err := os.Open(corpus + "manifest.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	c := compiler.New()
	paths, err := compiler.GatherPaths([]string{"../../../../tables/examples"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.Lookup("ProfileConfig")
	if root == nil {
		t.Fatal("tabletext has no ProfileConfig root")
	}

	s := bufio.NewScanner(manifest)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("malformed manifest row %q", line)
		}
		text, err := os.ReadFile(corpus + cols[0])
		if err != nil {
			t.Fatal(err)
		}
		wantRead := cols[1] == "read"
		inst := m.New(root)
		var engineReport tabletext.Report
		engineRead := m.Read(inst, text, &engineReport)
		if engineRead != wantRead {
			t.Errorf("tabletext reference for %s: read=%v report=%+v, manifest wants read=%v", cols[0], engineRead, engineReport, wantRead)
			continue
		}
		var generated tabledemo.ProfileConfig
		var generatedReport tabledemo.TableReport
		generatedRead := tabledemo.ProfileConfigFromJSON(&generated, text, &generatedReport)
		if generatedRead != engineRead {
			t.Errorf("%s: generated read=%v reason=%q, tabletext read=%v reason=%q", cols[0], generatedRead, generatedReport.Reason, engineRead, engineReport.Reason)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}
