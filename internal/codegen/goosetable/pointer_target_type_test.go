package goosetable_test

// POINTER-TARGET-TYPE, GOOSE (docs/SPEC-TABLES.md §3.1; docs/rulings.json
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
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type pointerTargetCase struct {
	name, unit, root, file, verdict string
	unknown                         int // -1 when the row does not pin one
}

func pointerTargetCases(t *testing.T) []pointerTargetCase {
	t.Helper()
	path := filepath.Join(root, "testdata", "wire", "pointer-target-type", "MANIFEST.tsv")
	mf, err := os.Open(path)
	if err != nil {
		t.Fatalf("pointer-target-type manifest: %v", err)
	}
	defer mf.Close()
	var rows []pointerTargetCase
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
		row := pointerTargetCase{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
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

// pointerTargetProbe is the program's per-row body: the plain read and the
// retaining read of one wire, each printing its verdict, its unknown count and
// whether the root's second field (`next`) is null.
const pointerTargetProbe = `
fn pointer_target_case(name: const u8[:], file: const u8[:], root_name: const u8[:]) {
    let data = input(file);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), root_name, data, vals, nodes, report);
    if root >= 0 && !report.refused {
        let slot = vals[root].first + 1;
        let is_null = vals[slot].bits.lo == 0 && vals[slot].bits.hi == 0 && vals[slot].state != schema_rt::ValPresent;
        write_stdout(str(name, " plain read ", report.unknown, " ", if is_null { "null" } else { "present" }, "\n"));
    } else {
        write_stdout(str(name, " plain refused\n"));
    }
    var rvals: schema_rt::LoadVal[>..] = [];
    var rnodes: schema_rt::LoadNode[>..] = [];
    var rreport = schema_rt::LoadReport { .. };
    var rbytes: u8[>..<] = [];
    var rrecs: schema_rt::RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: 65536, id_capacity: 1024, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let rroot = schema_rt::load_table_retain(conformance_unit(), root_name, data, rvals, rnodes, rreport, store);
    if rroot >= 0 && !rreport.refused {
        let slot = rvals[rroot].first + 1;
        let is_null = rvals[slot].bits.lo == 0 && rvals[slot].bits.hi == 0 && rvals[slot].state != schema_rt::ValPresent;
        write_stdout(str(name, " retain read ", rreport.unknown, " ", if is_null { "null" } else { "present" }, "\n"));
    } else {
        write_stdout(str(name, " retain refused\n"));
    }
}
`

// pointerTargetAnswer is one read entry's answer for one vector.
type pointerTargetAnswer struct {
	verdict string
	unknown int
	null    bool
}

func TestGoosePointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the generated reader)")

	rows := pointerTargetCases(t)
	byUnit := map[string][]pointerTargetCase{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	for unit, unitRows := range byUnit {
		gathered, err := compiler.GatherPaths(prefix([]string{unit}))
		if err != nil {
			t.Fatalf("%s: %v", unit, err)
		}
		u, err := compiler.New().Load(gathered)
		if err != nil {
			t.Fatalf("%s does not load: %v", unit, err)
		}
		files := probeFiles(t, u, nil)
		var main strings.Builder
		main.WriteString(probePrelude)
		main.WriteString(pointerTargetProbe)
		main.WriteString("\nfn main() {\n")
		dir := filepath.Join(root, "testdata", "wire", "pointer-target-type")
		for _, row := range unitRows {
			wire, err := os.ReadFile(filepath.Join(dir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			files[row.file] = wire
			fmt.Fprintf(&main, "    pointer_target_case(%q, %q, %q);\n", row.name, row.file, row.root)
		}
		main.WriteString("}\n")
		files["main.goose"] = []byte(main.String())
		out := gooserun.Run(t, t.TempDir(), "main.goose", files)

		got := map[string]pointerTargetAnswer{}
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.Fields(line)
			switch {
			case len(f) == 3 && f[2] == "refused":
				got[f[0]+" "+f[1]] = pointerTargetAnswer{verdict: "refused"}
			case len(f) == 5 && f[2] == "read":
				n, err := strconv.Atoi(f[3])
				if err != nil {
					t.Fatalf("%s: %q is not a count", line, f[3])
				}
				got[f[0]+" "+f[1]] = pointerTargetAnswer{verdict: "read", unknown: n, null: f[4] == "null"}
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
