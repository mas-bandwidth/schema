package rusttable

// POINTER-TARGET-TYPE, RUST (docs/SPEC-TABLES.md §3.1; docs/rulings.json
// pointer-target-type-checked): in the variable form, a pointer slot that
// names a node record whose type this reader DECLARES but which is not the
// type the pointer field declares is damage and the whole load refuses. A
// record whose type this reader cannot name at all — a newer writer's type —
// stays lawful: it is counted unknown once and every pointer naming it reads
// null. The vectors are testdata/wire/pointer-target-type; every row runs
// through the plain and the retaining read, and a read row's report is held to
// the manifest's unknown count. The unknown-type row also holds its pointer to
// null.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type rustPointerTargetCase struct {
	name, unit, root, file, verdict string
	unknown                         int // -1 when the row does not pin one
}

func rustPointerTargetCases(t *testing.T, repo string) []rustPointerTargetCase {
	t.Helper()
	path := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "MANIFEST.tsv")
	mf, err := os.Open(path)
	if err != nil {
		t.Fatalf("pointer-target-type manifest: %v", err)
	}
	defer mf.Close()
	var rows []rustPointerTargetCase
	sc := bufio.NewScanner(mf)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("%s: %d fields, want 6", path, len(f))
		}
		row := rustPointerTargetCase{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("%s: verdict %q, want read or refused", path, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("%s: unknown %q: %v", path, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s names no vectors", path)
	}
	return rows
}

// rustPointerTargetAnswer is one read entry's answer for one vector.
type rustPointerTargetAnswer struct {
	verdict string
	unknown int
	null    bool
}

func TestRustPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it builds and runs the generated Rust)")
	if _, err := exec.LookPath("rustc"); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and rustc is not on PATH: %v", err)
		}
		t.Skipf("rustc is not on PATH: %v", err)
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	rows := rustPointerTargetCases(t, repo)
	byUnit := map[string][]rustPointerTargetCase{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	dir := filepath.Join(repo, "testdata", "wire", "pointer-target-type")

	for unit, unitRows := range byUnit {
		files, err := Generate(vcLoadUnit(t, repo, unit))
		if err != nil {
			t.Fatalf("%s: generate: %v", unit, err)
		}
		var body strings.Builder
		for i, row := range unitRows {
			wire, err := os.ReadFile(filepath.Join(dir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			fmt.Fprintf(&body, "const WIRE_%d: [u8; %d] = [%s];\n", i, len(wire), rustBytes(wire))
		}
		for i, row := range unitRows {
			fn := ir.RustSnake(row.root)
			fmt.Fprintf(&body, `
{
    let wire = &WIRE_%d[..];
    let mut report = TableReport::default();
    let v = %s_load(wire, &mut report);
    if v.is_some() && !report.damaged {
        let is_null = v.as_ref().is_none_or(|n| n.next.is_none());
        println!("%s plain read {} {}", report.unknown, if is_null { "null" } else { "present" });
    } else {
        println!("%s plain refused");
    }
    let mut retain = TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() };
    let mut rreport = TableReport::default();
    let rv = %s_load_retain(wire, &mut retain, &mut rreport);
    if rv.is_some() && !rreport.damaged {
        let is_null = rv.as_ref().is_none_or(|n| n.next.is_none());
        println!("%s retain read {} {}", rreport.unknown, if is_null { "null" } else { "present" });
    } else {
        println!("%s retain refused");
    }
}
`, i, fn, row.name, row.name, fn, row.name, row.name)
		}
		out, err := runRustGeneratedFilesProbe(t, files, body.String())
		if err != nil {
			t.Fatalf("%s: pointer target probe failed: %v\n%s", unit, err, out)
		}
		got := map[string]rustPointerTargetAnswer{}
		for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			f := strings.Fields(line)
			switch {
			case len(f) == 3 && f[2] == "refused":
				got[f[0]+" "+f[1]] = rustPointerTargetAnswer{verdict: "refused"}
			case len(f) == 5 && f[2] == "read":
				n, err := strconv.Atoi(f[3])
				if err != nil {
					t.Fatalf("%s: %q is not a count", line, f[3])
				}
				got[f[0]+" "+f[1]] = rustPointerTargetAnswer{verdict: "read", unknown: n, null: f[4] == "null"}
			default:
				t.Fatalf("the probe printed %q", line)
			}
		}
		for _, row := range unitRows {
			for _, entry := range []string{"plain", "retain"} {
				answer, ok := got[row.name+" "+entry]
				if !ok {
					t.Errorf("%s %s: the probe printed no answer", row.name, entry)
					continue
				}
				if answer.verdict != row.verdict {
					t.Errorf("%s %s: verdict %q, the manifest says %q", row.name, entry, answer.verdict, row.verdict)
					continue
				}
				if row.verdict != "read" {
					continue
				}
				if row.unknown >= 0 && answer.unknown != row.unknown {
					t.Errorf("%s %s: unknown %d, the manifest says %d", row.name, entry, answer.unknown, row.unknown)
				}
				if row.name == "next-writer-only" && !answer.null {
					t.Errorf("%s %s: the pointer did not read null", row.name, entry)
				}
			}
		}
	}
}
