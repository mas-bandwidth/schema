package goosetable_test

// THE TABLE WIRE's gate (docs/SPEC-TABLES.md §3, §4, §6.6; docs/PORT-GOOSE.md,
// THE TABLE WIRE): every unit of testdata/conformance/tables/MANIFEST.txt is
// generated for Goose into a temp dir, the conformance program
// (test/conformance/goose/main.goose) runs beside it under the pinned
// compiler, and its answers are held to the corpus — the C++ reference's
// bytes and the generated reports — exactly as the harness holds them:
//
//	wire         every instance loaded and saved back, byte for byte, and a
//	             writer refusal (go-refusal=) given by its name
//	report       every report row's §4 counters and verdict (reports.txt)
//	retain       the load's retained,retain_lost,unknown and the save's lost
//	retain-save  the bytes the retaining save writes back
//
// TestGooseTableWireNegativeControl breaks the runtime three ways, one per
// walk, and requires each break to turn red exactly the rows it owns.
//
// Unit tests here use no real time, no socket and no network: each run is
// `goose run` of a generated program in t.TempDir().

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// wireCorpus is the manifest's rows the four surfaces read, with their
// expectations resolved.
type wireCorpus struct {
	units    map[string][]string // key -> schema paths
	order    []string
	manifest string                       // the manifest with every path absolute
	want     map[string]map[string]string // surface -> case -> expected answer ("absent <reason>" for a named absence)
	unitOf   map[string]map[string]string // surface -> case -> unit
}

func readWireCorpus(t *testing.T) *wireCorpus {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	reports := map[string]string{}
	rf, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(rf), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && !strings.HasPrefix(f[0], "#") {
			reports[f[0]] = f[1]
		}
	}
	mf, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer mf.Close()
	c := &wireCorpus{units: map[string][]string{}, want: map[string]map[string]string{}, unitOf: map[string]map[string]string{}}
	for _, s := range []string{"wire", "report", "retain", "retain-save"} {
		c.want[s] = map[string]string{}
		c.unitOf[s] = map[string]string{}
	}
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	var m strings.Builder
	sc := bufio.NewScanner(mf)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "unit" && len(f) >= 3:
			c.units[f[1]] = f[2:]
			c.order = append(c.order, f[1])
		case f[0] == "instance" && len(f) >= 5:
			want := read(f[4])
			for _, marker := range f[5:] {
				if r, ok := strings.CutPrefix(marker, "go-refusal="); ok {
					want = "absent " + r
				}
			}
			c.want["wire"][f[1]], c.unitOf["wire"][f[1]] = want, f[2]
			fmt.Fprintf(&m, "instance %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]))
		case f[0] == "report" && len(f) >= 5:
			c.want["report"][f[1]], c.unitOf["report"][f[1]] = reports[f[1]]+"\n", f[2]
			fmt.Fprintf(&m, "report %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]))
		case f[0] == "retain" && len(f) >= 10:
			c.want["retain"][f[1]], c.unitOf["retain"][f[1]] = f[7]+" "+f[8]+"\n", f[2]
			if f[9] != "-" {
				var saves strings.Builder
				for p := range strings.SplitSeq(f[9], ",") {
					saves.WriteString(read(p))
				}
				c.want["retain-save"][f[1]], c.unitOf["retain-save"][f[1]] = saves.String(), f[2]
			}
			fmt.Fprintf(&m, "retain %s %s %s %s %s %s\n", f[1], f[2], f[3], filepath.Join(abs, f[4]), f[5], f[6])
		case f[0] == "retain-message" && len(f) >= 5:
			// the form-2 row names a variable table root, which rides no
			// message form (§3.3): absent by the Go leg's reason
			c.want["retain"][f[1]], c.unitOf["retain"][f[1]] = "absent variable table: §3.3", f[3]
			fmt.Fprintf(&m, "retain-message %s %s %s\n", f[1], f[2], f[3])
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	c.manifest = m.String()
	return c
}

// wireUnitFiles generates one unit for Goose: the type and table surfaces,
// the runtime (with `override` replacing runtime files), the conformance
// program and its glue module.
func wireUnitFiles(t *testing.T, key string, paths []string, override map[string][]byte) map[string][]byte {
	t.Helper()
	gathered, err := compiler.GatherPaths(prefix(paths))
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		t.Fatalf("%s does not load: %v", key, err)
	}
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("%s: goose: %v", key, err)
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		t.Fatalf("%s: goosetable: %v", key, err)
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
	// the message surface's arm (the shell driver writes one per root); these
	// gates drive no message row, so the glue carries none
	glue.WriteString("fn conformance_message(root: const u8[:], announce: const u8[:], wire: const u8[:], out: u8[>..]&) -> i64 { return -1; }\n")
	files["conformance_unit.goose"] = []byte(glue.String())
	main, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "main.goose"))
	if err != nil {
		t.Fatal(err)
	}
	files["main.goose"] = main
	// the unit's text glue, as the shell driver places it
	text, err := os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "units", key+".goose"))
	if errors.Is(err, fs.ErrNotExist) {
		text, err = os.ReadFile(filepath.Join(root, "test", "conformance", "goose", "unit.goose"))
	}
	if err != nil {
		t.Fatal(err)
	}
	files["unit.goose"] = text
	return files
}

// runWireSurfaces runs `surfaces` over every unit that has a row on one of
// them and answers surface -> case -> answer.
func runWireSurfaces(t *testing.T, c *wireCorpus, surfaces []string, override map[string][]byte) map[string]map[string]string {
	t.Helper()
	got := map[string]map[string]string{}
	for _, s := range surfaces {
		got[s] = map[string]string{}
	}
	var mu sync.Mutex
	t.Run("units", func(t *testing.T) {
		for _, key := range c.order {
			var mine []string
			for _, s := range surfaces {
				for _, u := range c.unitOf[s] {
					if u == key {
						mine = append(mine, s)
						break
					}
				}
			}
			if len(mine) == 0 {
				continue
			}
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				files := wireUnitFiles(t, key, c.units[key], override)
				files["manifest.txt"] = []byte(c.manifest)
				for _, s := range mine {
					out := gooserun.RunArgs(t, dir, "main.goose", files, s, "manifest.txt", key)
					mu.Lock()
					for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
						name, h, ok := strings.Cut(line, " ")
						if !ok {
							continue
						}
						if rest, absent := strings.CutPrefix(h, "absent"); absent {
							got[s][name] = "absent " + strings.TrimPrefix(rest, " ")
							continue
						}
						b, err := hex.DecodeString(h)
						if err != nil {
							t.Errorf("%s %s: %q is not hex", s, name, h)
						}
						got[s][name] = string(b)
					}
					mu.Unlock()
				}
			})
		}
	})
	return got
}

// failing is the rows of one surface whose answer is not the expectation.
func failing(c *wireCorpus, got map[string]map[string]string, surface string) []string {
	var out []string
	for name, want := range c.want[surface] {
		if got[surface][name] != want {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

var wireSurfaces = []string{"wire", "report", "retain", "retain-save"}

// TestGooseTableWireMatchesTheCorpus is the gate: every row of the four
// surfaces answers what the C++ reference and the generated reports pin.
func TestGooseTableWireMatchesTheCorpus(t *testing.T) {
	t.Parallel()
	c := readWireCorpus(t)
	got := runWireSurfaces(t, c, wireSurfaces, nil)
	for _, s := range wireSurfaces {
		bad := failing(c, got, s)
		for _, name := range bad {
			t.Errorf("%s / %s: got %q, want %q", s, name, clip(got[s][name]), clip(c.want[s][name]))
		}
		t.Logf("%s: %d/%d rows match", s, len(c.want[s])-len(bad), len(c.want[s]))
	}
}

func clip(s string) string {
	if len(s) > 48 {
		return fmt.Sprintf("%x… (%d bytes)", s[:24], len(s))
	}
	return s
}

// sabotage replaces one line of a runtime file, failing if it is not there
// exactly once, so a control can never pass vacuously over moved code.
func sabotage(t *testing.T, file, from, to string) map[string][]byte {
	t.Helper()
	data := goosetable.Runtime()[file]
	if n := strings.Count(string(data), from); n != 1 {
		t.Fatalf("sabotage: %s holds %q %d times, want once", file, from, n)
	}
	return map[string][]byte{file: []byte(strings.Replace(string(data), from, to, 1))}
}

// TestGooseTableWireNegativeControl breaks each walk once and requires the
// break to turn red only the rows it owns, and at least one of them:
//
//   - the LOAD stops counting a field WIDENED in a body (§4): only report rows
//     whose pinned widened counter is nonzero go red, and wire stays green;
//   - the SAVE keeps the id of an elided nested table (§3: an elided field
//     costs nothing in the id table either): only wire rows go red;
//   - RETENTION writes no retained tail (§6.6): only retain-save and retain
//     rows go red, and wire and report stay green.
//   - the SAVE numbers each node one past its first-visit index (§3.1, M6):
//     only wire rows go red;
//   - the SAVE writes a multi-byte value high byte first (M12: byte order is
//     the format's, composed by shifts, never the host's): only wire rows go
//     red.
func TestGooseTableWireNegativeControl(t *testing.T) {
	t.Parallel()
	c := readWireCorpus(t)
	widened := func(name string) bool {
		f := strings.Split(strings.TrimSpace(c.want["report"][name]), ",")
		return len(f) == 7 && f[2] != "0"
	}
	controls := []struct {
		name, file, from, to string
		owns                 map[string]func(string) bool // surface -> may this row go red
		red                  string                       // the surface that must go red
	}{
		{"load-widened", "wire_load.goose",
			"if !entry || i != 0 { c.report.widened += 1; }", "if !entry || i != 0 { }",
			map[string]func(string) bool{"report": widened}, "report"},
		{"save-elided-id", "wire_save.goose",
			"            cut(c, at, mark);\n            return;\n        }\n        frame(c, start);",
			"            c.out.resize(at);\n            return;\n        }\n        frame(c, start);",
			map[string]func(string) bool{"wire": func(string) bool { return true }}, "wire"},
		{"retain-tail", "wire_retain.goose",
			"    if rec < 0 { return; }\n    for i in c.retain.recs.len {", "    if rec < 0 || rec >= 0 { return; }\n    for i in c.retain.recs.len {",
			map[string]func(string) bool{"retain": func(string) bool { return true }, "retain-save": func(string) bool { return true }}, "retain-save"},
		// M6: the save numbers a node one past the index its first visit
		// gives it, so every pointer edge names the wrong row of the node table
		{"save-numbering", "wire_save.goose",
			"    c.index[wire] = c.order.len + 1;", "    c.index[wire] = c.order.len + 2;",
			map[string]func(string) bool{"wire": func(string) bool { return true }}, "wire"},
		// M12: the save writes a multi-byte value high byte first, the
		// host-order write of a big-endian host the method refuses
		{"save-byte-order", "wire_save.goose",
			"        c.out.push((x & 0xFF) as! u8);\n        x = x >> 8;", "        c.out.push(((v >> ((8 * (w - 1 - i)) as! u64)) & 0xFF) as! u8);\n        x = x >> 8;",
			map[string]func(string) bool{"wire": func(string) bool { return true }}, "wire"},
	}
	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			var surfaces []string
			for _, s := range wireSurfaces {
				if s == "wire" || s == "report" || ctl.owns[s] != nil {
					surfaces = append(surfaces, s)
				}
			}
			got := runWireSurfaces(t, c, surfaces, sabotage(t, ctl.file, ctl.from, ctl.to))
			for _, s := range surfaces {
				for _, name := range failing(c, got, s) {
					if owns := ctl.owns[s]; owns == nil || !owns(name) {
						t.Errorf("%s: %s / %s went red, and the break does not own it", ctl.name, s, name)
					}
				}
			}
			if n := len(failing(c, got, ctl.red)); n == 0 {
				t.Errorf("%s: no %s row went red — the control is watching nothing", ctl.name, ctl.red)
			} else {
				t.Logf("%s: %d %s rows red, every other row green", ctl.name, n, ctl.red)
			}
		})
	}
}
