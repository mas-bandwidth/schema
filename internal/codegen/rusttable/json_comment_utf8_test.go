package rusttable

// docs/SPEC-TABLES.md §16.2: the bytes inside a JSON comment, line or block,
// are well-formed UTF-8 and hold no NUL. A comment that holds an ill-formed
// sequence or a NUL refuses the read, in a placed position and inside a
// skipped value under an unknown key. The engine (internal/tabletext) is the
// reference: every vector in the comment-text-rules corpus must answer the
// verdict the landed standard states there, and the emitted Rust reads it the
// same way.
//
// THIS IS A FUNCTIONAL TEST: it compiles and runs Rust with rustc. It skips by
// name when rustc is absent and SCHEMA_REQUIRE_CORPUS is unset, and fails by
// name when SCHEMA_REQUIRE_CORPUS is set.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func TestRustJsonCommentIsWellFormedUTF8(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the generated JSON comment probe)")

	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := generateUnit(t, repo, []string{"tables/examples"})
	model := tabletext.NewModel(u)
	st := model.Lookup("ProfileConfig")
	if st == nil {
		t.Fatal("no ProfileConfig table")
	}

	dir := filepath.Join(repo, "testdata", "wire", "json-comment-text-rules")
	manifest, err := os.Open(filepath.Join(dir, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()

	type row struct {
		name string
		text []byte
		read bool
	}
	var rows []row
	scanner := bufio.NewScanner(manifest)
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
		text, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		ok := model.Read(model.New(st), text, &report)
		engineRead := ok && !report.Refused
		if want := verdict == "read"; engineRead != want {
			t.Fatalf("%s: the engine answers %v where the landed standard reads %q (report %+v)", name, engineRead, verdict, report)
		}
		rows = append(rows, row{name: name, text: text, read: engineRead})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("manifest contains no comment-rule vectors")
	}

	modules, err := Generate(u)
	if err != nil {
		t.Fatalf("tables/examples was refused: %v", err)
	}
	root := t.TempDir()
	layoutRustUnit(t, root, "tabledemo", modules)

	var main strings.Builder
	main.WriteString("#![allow(dead_code, unused_imports)]\n")
	main.WriteString("mod tabledemo;\nuse tabledemo::*;\n")
	main.WriteString(`
fn verdict(text: &[u8]) -> bool {
    let mut r = TableReport::default();
    profile_config_from_json_bytes(text, &mut r).is_some() && !r.damaged
}

fn main() {
    let cases: &[(&str, &[u8], &str)] = &[
`)
	for _, r := range rows {
		want := "refused"
		if r.read {
			want = "read"
		}
		fmt.Fprintf(&main, "        (%q, &[%s], %q),\n", r.name, rustBytes(r.text), want)
	}
	main.WriteString(`
    ];
    for (name, text, want) in cases {
        let got = if verdict(text) { "read" } else { "refused" };
        if got != *want {
            println!("MISMATCH {name}: got {got}, want {want}");
        }
    }
    println!("PROBE DONE");
}
`)
	if err := os.WriteFile(filepath.Join(root, "main.rs"), []byte(main.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := rustcBuild(t, root)
	out, err := exec.Command(bin).CombinedOutput()
	for line := range strings.SplitSeq(string(out), "\n") {
		if line, ok := strings.CutPrefix(line, "MISMATCH "); ok {
			t.Errorf("%s", line)
		}
	}
	if err != nil {
		t.Fatalf("the comment-utf8 probe did not run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PROBE DONE") {
		t.Fatalf("the comment-utf8 probe did not finish:\n%s", out)
	}
}
