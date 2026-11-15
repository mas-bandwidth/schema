package luautable_test

// POINTER-TARGET-TYPE, LUAU (docs/SPEC-TABLES.md §3.1; docs/rulings.json
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
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
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

// pointerTargetAnswer is one read entry's answer for one vector.
type pointerTargetAnswer struct {
	verdict string
	unknown int
	null    bool
}

func TestLuauPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Luau interpreter (it runs the emitted readers)")

	rows := pointerTargetCases(t)
	byUnit := map[string][]pointerTargetCase{}
	for _, row := range rows {
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	for unit, unitRows := range byUnit {
		schemas, err := filepath.Glob(filepath.Join(root, unit, "*.schema"))
		if err != nil {
			t.Fatalf("%s: %v", unit, err)
		}
		if len(schemas) == 0 {
			t.Fatalf("%s declares no schema", unit)
		}
		src := map[string]string{}
		for _, path := range schemas {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			src[filepath.Base(path)] = string(data)
		}
		g := generateFiles(t, src)

		var mods []string
		for name := range g.files {
			if strings.HasSuffix(name, "Table.luau") {
				mods = append(mods, strings.TrimSuffix(name, ".luau"))
			}
		}
		sort.Strings(mods)

		var main strings.Builder
		main.WriteString("local L = require(\"./schema_load\")\nlocal R = require(\"./schema_retain\")\nlocal inputs = require(\"./inputs\")\n")
		for _, mod := range mods {
			fmt.Fprintf(&main, "require(%q)\n", "./"+mod)
		}
		main.WriteString("local function answer(name: string, bytes: buffer, root: string)\n")
		main.WriteString("\tlocal report: any = {}\n")
		main.WriteString("\tlocal v = L.load(root, bytes, report)\n")
		main.WriteString("\tif v ~= nil then\n")
		main.WriteString("\t\tprint(name .. \" plain read \" .. tostring(report.unknown) .. \" \" .. (if v.Next == nil then \"null\" else \"present\"))\n")
		main.WriteString("\telse\n")
		main.WriteString("\t\tprint(name .. \" plain refused\")\n")
		main.WriteString("\tend\n")
		main.WriteString("\tlocal rr: any = {}\n")
		main.WriteString("\tlocal rv = R.LoadRetain(root, bytes, rr, 65536)\n")
		main.WriteString("\tif rv ~= nil then\n")
		main.WriteString("\t\tprint(name .. \" retain read \" .. tostring(rr.unknown) .. \" \" .. (if rv.Next == nil then \"null\" else \"present\"))\n")
		main.WriteString("\telse\n")
		main.WriteString("\t\tprint(name .. \" retain refused\")\n")
		main.WriteString("\tend\n")
		main.WriteString("end\n")

		dir := filepath.Join(root, "testdata", "wire", "pointer-target-type")
		inputs := map[string][]byte{}
		for _, row := range unitRows {
			wire, err := os.ReadFile(filepath.Join(dir, row.file))
			if err != nil {
				t.Fatalf("%s: %v", row.file, err)
			}
			inputs[row.name] = wire
			fmt.Fprintf(&main, "answer(%q, inputs[%q], %q)\n", row.name, row.name, row.root)
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(main.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		out := luaurun.Run(t, g.dir, "main.luau", inputs)

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
