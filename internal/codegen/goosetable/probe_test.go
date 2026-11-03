package goosetable_test

// THE PROBE UNIT: one schema source generated for Goose into a program
// directory, beside a main the test writes, so a gate can drive the runtime
// (schema_rt::load_table, load_table_retain, save_table, save_table_retain)
// over a unit of its own. The oracle on the Go side is the reference engine
// (internal/tabletext, internal/tablewire), which shares no code with the
// Goose runtime.

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// probeUnit loads one schema source as a unit.
func probeUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the probe schema does not load: %v", err)
	}
	return u
}

// probeFiles generates u for Goose (with `override` replacing runtime files)
// and a glue module, conformance_unit.goose, that imports every generated
// module and answers the unit as data, so a main needs one import.
func probeFiles(t *testing.T, u *ir.Unit, override map[string][]byte) map[string][]byte {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose: %v", err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("goosetable: %v", err)
	}
	maps.Copy(files, tables)
	maps.Copy(files, override)
	mods := make([]string, 0, len(files))
	for name := range files {
		mods = append(mods, strings.TrimSuffix(name, ".goose"))
	}
	sort.Strings(mods)
	var glue strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&glue, "import .%s;\n", m)
	}
	fmt.Fprintf(&glue, "fn conformance_unit() -> schema_rt::LoadUnit { return %s::%s; }\n", goose.Namespace(u), goosetable.UnitName)
	files["conformance_unit.goose"] = []byte(glue.String())
	return files
}

// probePrelude is the probe mains' shared head: the imports and a hex line.
const probePrelude = `import os;
import .conformance_unit;
import .wire_load;
import .wire_retain;
import .wire_save;

fn hex(b: const u8[:]) -> u8[>..] {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    for x in b {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    if line.len == 0 { line.push('-'); }
    return line;
}

fn input(name: const u8[:]) -> u8[>..] {
    var data: u8[>..] = [];
    if !read_file(name, data) { write_stderr(str("probe: cannot read ", name, "\n")); exit(1); }
    return data;
}
`

// probeHex reads a hex word a probe printed ("-" is no bytes).
func probeHex(t *testing.T, word string) []byte {
	t.Helper()
	if word == "-" {
		return nil
	}
	b, err := hex.DecodeString(word)
	if err != nil {
		t.Fatalf("probe printed %q, which is not hex: %v", word, err)
	}
	return b
}

// probeLines splits a probe's stdout into lines of words, keyed by the first.
func probeLines(out []byte) map[string][]string {
	lines := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 {
			lines[f[0]] = f[1:]
		}
	}
	return lines
}
