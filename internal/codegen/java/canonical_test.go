package java

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE CANONICAL PASS MOVES LAYOUT AND NEVER A TOKEN, on the packet-wire
// units (issue #547, docs/JAVA-DIALECT.md): over the corpora the format gate
// formats, Canonical changes only whitespace and comments (and drops only
// imports nothing names), and a second pass changes nothing. It reads the
// generator's output and runs no Java.
func TestCanonicalMovesLayoutNotTokens(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	// the units make/java.mk generates into build/java-clean-packet
	units := [][]string{
		{"examples"}, {"examples128"},
		{"bench/corpus/Bench.schema"}, {"bench/corpus/RealWorld.schema"},
		{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"},
		{"test/packet-defaults/Defaults.schema"}, {"test/packet-defaults/Plain.schema"},
		{"test/packet-wide/Shapes.schema"},
	}
	for _, paths := range units {
		d := strings.Join(paths, "+")
		u := packetUnit(t, root, paths)
		raw, err := generate(u, func(b []byte) []byte { return b })
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		canon, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		for name, before := range raw {
			after := canon[name]
			kept := []string{}
			for _, l := range strings.Split(string(before), "\n") {
				if strings.HasPrefix(l, "import ") && !strings.Contains(string(after), l+"\n") {
					continue
				}
				kept = append(kept, l)
			}
			if CodeTokens(string(after)) != CodeTokens(strings.Join(kept, "\n")) {
				t.Errorf("%s/%s: the canonical pass changed a token, not only layout", d, name)
			}
			if again := Canonical(after); string(again) != string(after) {
				t.Errorf("%s/%s: the canonical pass is not idempotent", d, name)
			}
			if CommentText(string(after)) != CommentText(string(before)) {
				t.Errorf("%s/%s: the canonical pass changed the words of a comment", d, name)
			}
		}
	}
}

// packetUnit parses and checks one unit: each path is a .schema file, or a
// directory standing for every *.schema in it, the way `schema generate`
// reads its arguments.
func packetUnit(t *testing.T, root string, paths []string) *ir.Unit {
	t.Helper()
	var files []string
	for _, p := range paths {
		full := filepath.Join(root, p)
		if strings.HasSuffix(p, ".schema") {
			files = append(files, full)
			continue
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".schema") {
				files = append(files, filepath.Join(full, e.Name()))
			}
		}
	}
	var sources []check.SourceFile
	for _, p := range files {
		name := filepath.Base(p)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(p, b)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", p, perrs[0])
		}
		sources = append(sources, check.SourceFile{Path: p, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: b, AST: f})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// RELAYOUT LAYS OUT A COMPACT TEMPLATE AND NOTHING ELSE (#547): a statement
// group an emitter wrote on one line, unspaced, comes out one statement a
// line in the block's indentation; a line already in the formatter's shape
// keeps even a wrong indentation, so the drift control still sees it.
func TestRelayoutLaysOutCompactTemplatesOnly(t *testing.T) {
	t.Parallel()
	in := []string{
		"    static void f(Report report, Reader r, int kind) {",
		"        if (kind == 2) {",
		"            r.skip(kind);",
		"        } else if (kind == 15) {",
		"if(kind==15){v.value=X.readPayload(r,nt,report);}else{r.skip(kind);report.kindMismatch++;}",
		"        }",
		"         int misIndented = 0;",
		"        switch (kind) {",
		"            case 1:w.putLeb(0);break;",
		"        }",
		"    }",
	}
	want := []string{
		"    static void f(Report report, Reader r, int kind) {",
		"        if (kind == 2) {",
		"            r.skip(kind);",
		"        } else if (kind == 15) {",
		"            if (kind == 15) {",
		"                v.value = X.readPayload(r, nt, report);",
		"            } else {",
		"                r.skip(kind);",
		"                report.kindMismatch++;",
		"            }",
		"        }",
		"         int misIndented = 0;",
		"        switch (kind) {",
		"            case 1:",
		"                w.putLeb(0);",
		"                break;",
		"        }",
		"    }",
	}
	got := relayout(in)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("relayout:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if again := relayout(got); strings.Join(again, "\n") != strings.Join(got, "\n") {
		t.Fatalf("relayout is not idempotent:\n%s", strings.Join(again, "\n"))
	}
	if CodeTokens(strings.Join(got, "\n")) != CodeTokens(strings.Join(in, "\n")) {
		t.Fatalf("relayout changed a token")
	}
}
