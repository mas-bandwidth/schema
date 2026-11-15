package tabletext_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestTextJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	const src = "package probe\ntable Root\n{\n    value int32\n}\n"
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	m := tabletext.NewModel(u)
	st := m.Lookup("Root")
	if st == nil {
		t.Fatal("no table Root")
	}

	manifest, err := os.Open("../../testdata/wire/json-comment-text-rules/manifest.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	scanner := bufio.NewScanner(manifest)
	rows := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			t.Fatalf("bad manifest line %q", line)
		}
		name, verdict := cols[0], cols[1]
		if verdict != "read" && verdict != "refused" {
			t.Fatalf("bad manifest verdict %q for %s", verdict, name)
		}
		rows++
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			text, err := os.ReadFile(filepath.Join("../../testdata/wire/json-comment-text-rules", name))
			if err != nil {
				t.Fatal(err)
			}
			var report tabletext.Report
			ok := m.Read(m.New(st), text, &report)
			if verdict == "read" && (!ok || report.Refused) {
				t.Fatalf("manifest expects read, got ok=%v report=%+v", ok, report)
			}
			if verdict == "refused" && (ok || !report.Refused) {
				t.Fatalf("manifest expects refusal, got ok=%v report=%+v", ok, report)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Fatal("manifest contains no comment-rule vectors")
	}
}
