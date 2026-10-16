package darttable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// retainRootFamily is one construct family the Dart leg carries on the
// variable form (docs/SPEC-TABLES.md §2, §3): the unit's declaration directory,
// the root, the committed wire a newer writer starts from, and the Dart prefix
// of the root's verbs.
type retainRootFamily struct {
	family, dir, root, seed, prefix string
}

// retainRootFamilies is one root per construct family: pointers and their node
// table (§3.1), *string/*bytes blobs (§2.5), unbounded lists (§2.9), and the
// sprint card's own record (tables/cards), the use case the retention is for.
var retainRootFamilies = []retainRootFamily{
	{"pointers", "tables/pointers", "Scene", "testdata/wire/tables/graph_tree.bin", "scene"},
	{"blobs", "tables/blobs", "Catalog", "testdata/wire/tables/blob_small.bin", "catalog"},
	{"lists", "tables/lists", "Save", "testdata/wire/tables/list_tables.bin", "save"},
	{"cards", "tables/cards", "Card", "tables/cards/fuzz/card-populated.wire", "card"},
}

// repoPath is a repository-relative path from this package's directory.
func repoPath(rel string) string { return filepath.Join("..", "..", "..", filepath.FromSlash(rel)) }

// loadRetainDirUnit checks every .schema file of one declaration directory as one
// unit, applying edit to the file that declares `table <root>` so a test can
// spell a NEWER writer of the same unit without a committed copy.
func loadRetainDirUnit(t *testing.T, dir, root string, edit func(string) string) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repoPath(dir), "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no .schema files under %s: %v", dir, err)
	}
	tmp := t.TempDir()
	var out []string
	edited := false
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if edit != nil && strings.Contains(text, "table "+root+"\n{\n") {
			text = edit(text)
			edited = true
		}
		dst := filepath.Join(tmp, filepath.Base(p))
		if err := os.WriteFile(dst, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		out = append(out, dst)
	}
	if edit != nil && !edited {
		t.Fatalf("no file under %s declares table %s", dir, root)
	}
	return loadUnit(t, out...)
}

// dartWireProbe generates the unit's Dart files into a temp dir, writes the
// probe beside them with HOME replaced by the unit's wire library, and runs it.
func dartWireProbe(t *testing.T, u *ir.Unit, probe string, args ...string) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Dart should generate the unit: %v", err)
	}
	dir := t.TempDir()
	home := ""
	for name, src := range files {
		if strings.HasSuffix(name, "Wire.dart") {
			home = name
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if home == "" {
		t.Fatal("Generate emitted no wire library")
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(strings.ReplaceAll(probe, "HOME", home)), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dart probe: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// goReportLine is the probe's line for the Go engine's report, so the two legs
// compare as one string: the six counters, the two retention counters and the
// verdict's reason in the reference vocabulary (gotable/refusal.go).
func goReportLine(r tabletext.Report) string {
	reason := "ok"
	switch {
	case r.Refused:
		reason = "unknown_form"
	case r.Malformed:
		reason = "wire_damaged"
	}
	return fmt.Sprintf("%d %d %d %d %v %v %d %d %s", r.Unknown, r.KindMismatch, r.Widened, r.Clamped,
		r.Malformed, r.Refused, r.Retained, r.RetainLost, reason)
}

// signed sets a signed integer cell the way the Go engine's decoder fills one.
func signed(v int64) func(*tabletext.Cell) {
	return func(c *tabletext.Cell) { c.I = v; c.U = uint64(v) }
}

const dartReportLine = `'${r.unknown} ${r.kindMismatch} ${r.widened} ${r.clamped} ${r.malformed} ${r.refused} ${r.retained} ${r.retainLost} ${r.reason}'`

// setField sets one scalar field of an instance by name (the Go engine's
// value model, internal/tabletext, whose encoder writes an integer from its
// two's-complement U and elides one whose I is the default), failing the test
// when there is none.
func setField(t *testing.T, inst *tabletext.Instance, name string, set func(*tabletext.Cell)) {
	t.Helper()
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			inst.Fields[i].Present = true
			set(&inst.Fields[i].Cell)
			return
		}
	}
	t.Fatalf("%s has no field %s", inst.Def.Name, name)
}

// TestRetainsAndWidensOnEveryRootDart pins docs/SPEC-TABLES.md §4 and §6.6 for
// the Dart leg on every construct family it carries, against the Go engine's
// bytes (internal/tablewire, the conformance harness's oracle):
//
//   - a DIRECT WIDENING: a narrower writer's int8, int16 and uint8 read into a
//     reader's int32, int64 and uint32 are widened (three counted), the values
//     hold, and the reader's save is the Go engine's save byte for byte;
//   - the report's REASON in the reference vocabulary (gotable/refusal.go):
//     ok on a read, unknown_form on a form byte this reader does not carry,
//     wire_damaged on a wire that cannot be read whole;
//   - a RETAINED ROUND TRIP, one root per family: a newer writer adds a field
//     the reader cannot name, the reader's retaining load keeps it (one
//     retained, none lost), its retaining save is the Go engine's byte for byte,
//     and the newer writer reads the field back from that save.
func TestRetainsAndWidensOnEveryRootDart(t *testing.T) {
	t.Parallel()
	t.Run("enum-none-keeps-later-fields", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, "package none\n\nenum Mode { Alpha, Beta }\n\ntable Probe\n{\n    a int32\n    mode Mode = Beta\n    later int32\n}\n")
		m := tabletext.NewModel(u)
		inst := m.New(m.Lookup("Probe"))
		setField(t, inst, "a", signed(7))
		setField(t, inst, "mode", func(c *tabletext.Cell) { c.U = 0 })
		setField(t, inst, "later", signed(9))
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		in, out := filepath.Join(dir, "none.wire"), filepath.Join(dir, "saved.wire")
		if err := os.WriteFile(in, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		got := dartWireProbe(t, u, `import 'dart:io';
import 'HOME';
void main(List<String> args) {
  final report = TableWireReport();
  final value = probeWireLoad(File(args[0]).readAsBytesSync(), report);
  File(args[1]).writeAsBytesSync(probeWireSave(value));
  stdout.writeln('${value.a} ${value.mode} ${value.later} ${report.malformed}');
}
`, in, out)
		if got != "7 0 9 false" {
			t.Errorf("None and the following field: %q", got)
		}
		saved, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, wire) {
			t.Errorf("Dart saved a different value after None: got %x, want %x", saved, wire)
		}
	})

	t.Run("widening", func(t *testing.T) {
		t.Parallel()
		reader := unitFrom(t, "package widen\n\ntable Probe\n{\n    a int32\n    b int64\n    c uint32\n}\n")
		writer := unitFrom(t, "package widen\n\ntable Probe\n{\n    a int8\n    b int16\n    c uint8\n}\n")
		wm := tabletext.NewModel(writer)
		winst := wm.New(wm.Lookup("Probe"))
		setField(t, winst, "a", signed(-5))
		setField(t, winst, "b", signed(-300))
		setField(t, winst, "c", func(c *tabletext.Cell) { c.U = 200 })
		narrow, err := tablewire.Encode(wm, winst)
		if err != nil {
			t.Fatal(err)
		}

		rm := tabletext.NewModel(reader)
		rinst := rm.New(rm.Lookup("Probe"))
		var rep tabletext.Report
		if _, err := tablewire.Decode(rm, rinst, narrow, &rep); err != nil {
			t.Fatal(err)
		}
		goSave, err := tablewire.Encode(rm, rinst)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Widened != 3 {
			t.Fatalf("the Go engine widened %d fields, want 3: the probe is not a direct widening", rep.Widened)
		}

		dir := t.TempDir()
		in, out := filepath.Join(dir, "narrow.wire"), filepath.Join(dir, "saved.wire")
		for _, w := range []struct {
			path string
			data []byte
		}{{in, narrow}, {filepath.Join(dir, "form7.wire"), []byte{7, 0, 0, 0, 0, 0, 0, 0, 0}}, {filepath.Join(dir, "short.wire"), []byte{1}}} {
			if err := os.WriteFile(w.path, w.data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got := dartWireProbe(t, reader, `import 'dart:io';
import 'HOME';
void main(List<String> args) {
  var r = TableWireReport();
  final v = probeWireLoad(File(args[0]).readAsBytesSync(), r);
  File(args[1]).writeAsBytesSync(probeWireSave(v));
  stdout.writeln('${v.a} ${v.b} ${v.c}');
  stdout.writeln(`+dartReportLine+`);
  for (final bad in [args[2], args[3]]) {
    r = TableWireReport();
    probeWireLoad(File(bad).readAsBytesSync(), r);
    stdout.writeln(`+dartReportLine+`);
  }
}
`, in, out, filepath.Join(dir, "form7.wire"), filepath.Join(dir, "short.wire"))
		lines := strings.Split(got, "\n")
		if len(lines) != 4 {
			t.Fatalf("the probe answered %d lines, want 4:\n%s", len(lines), got)
		}
		if lines[0] != "-5 -300 200" {
			t.Errorf("widened values: got %q, want %q", lines[0], "-5 -300 200")
		}
		if want := goReportLine(rep); lines[1] != want {
			t.Errorf("widening report: got %q, the Go engine's is %q", lines[1], want)
		}
		saved, _ := os.ReadFile(out)
		if !bytes.Equal(saved, goSave) {
			t.Errorf("the widened read's save differs from the Go engine's:\n got %x\nwant %x", saved, goSave)
		}
		for i, bad := range [][]byte{{7, 0, 0, 0, 0, 0, 0, 0, 0}, {1}} {
			var brep tabletext.Report
			_, _ = tablewire.Decode(rm, rm.New(rm.Lookup("Probe")), bad, &brep)
			if want := goReportLine(brep); lines[2+i] != want {
				t.Errorf("refusal report %d: got %q, the Go engine's is %q", i, lines[2+i], want)
			}
		}
		if !strings.HasSuffix(lines[2], " unknown_form") || !strings.HasSuffix(lines[3], " wire_damaged") {
			t.Errorf("the refusal reasons are not the reference vocabulary's: %q, %q", lines[2], lines[3])
		}
	})

	for _, fam := range retainRootFamilies {
		t.Run("retain/"+fam.family, func(t *testing.T) {
			t.Parallel()
			const extra = "zz_retained_probe"
			reader := loadRetainDirUnit(t, fam.dir, fam.root, nil)
			writer := loadRetainDirUnit(t, fam.dir, fam.root, func(src string) string {
				return strings.Replace(src, "table "+fam.root+"\n{\n", "table "+fam.root+"\n{\n    "+extra+" int32\n", 1)
			})
			seed, err := os.ReadFile(repoPath(fam.seed))
			if err != nil {
				t.Fatal(err)
			}

			// the NEWER writer: the committed value plus one field the reader
			// cannot name, written by the Go engine
			wm := tabletext.NewModel(writer)
			winst := wm.New(wm.Lookup(fam.root))
			var wrep tabletext.Report
			if _, err := tablewire.Decode(wm, winst, seed, &wrep); err != nil || wrep.Malformed || wrep.Refused {
				t.Fatalf("the newer writer cannot read the seed: %v %+v", err, wrep)
			}
			setField(t, winst, extra, signed(77))
			newer, err := tablewire.Encode(wm, winst)
			if err != nil {
				t.Fatal(err)
			}

			// the Go engine's retained round trip through the older reader
			rm := tabletext.NewModel(reader)
			rinst := rm.New(rm.Lookup(fam.root))
			retain := tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 17}
			var rep tabletext.Report
			if _, err := tablewire.DecodeRetain(rm, rinst, newer, &retain, &rep); err != nil {
				t.Fatal(err)
			}
			var srep tabletext.Report
			goSave, err := tablewire.EncodeRetain(rm, rinst, &retain, &srep)
			if err != nil {
				t.Fatal(err)
			}
			rep.RetainLost += srep.RetainLost
			if rep.Retained != 1 || rep.RetainLost != 0 {
				t.Fatalf("the Go engine retained %d and lost %d, want 1 and 0", rep.Retained, rep.RetainLost)
			}

			dir := t.TempDir()
			in, out := filepath.Join(dir, "newer.wire"), filepath.Join(dir, "saved.wire")
			if err := os.WriteFile(in, newer, 0o600); err != nil {
				t.Fatal(err)
			}
			got := dartWireProbe(t, reader, strings.ReplaceAll(`import 'dart:io';
import 'HOME';
void main(List<String> args) {
  final r = TableWireReport();
  final retain = TableRetain(1048576, List<int>.filled(131072, 0));
  final v = PREFIXWireLoadRetain(File(args[0]).readAsBytesSync(), retain, r);
  File(args[1]).writeAsBytesSync(PREFIXWireSaveRetain(v, retain, r));
  stdout.writeln(`+dartReportLine+`);
}
`, "PREFIX", fam.prefix), in, out)
			if want := goReportLine(rep); got != want {
				t.Errorf("%s.%s retained report: got %q, the Go engine's is %q", fam.dir, fam.root, got, want)
			}
			saved, _ := os.ReadFile(out)
			if !bytes.Equal(saved, goSave) {
				t.Errorf("%s.%s retained save differs from the Go engine's: got %d bytes, want %d\n got %x\nwant %x",
					fam.dir, fam.root, len(saved), len(goSave), saved, goSave)
			}

			// and the field survives the hop: the newer writer reads it back
			back := wm.New(wm.Lookup(fam.root))
			var brep tabletext.Report
			if _, err := tablewire.Decode(wm, back, saved, &brep); err != nil || brep.Malformed {
				t.Fatalf("the newer writer cannot read the retained save: %v %+v", err, brep)
			}
			for _, f := range back.Fields {
				if f.Def.Name == extra && f.Cell.I != 77 {
					t.Errorf("%s came back as %d, want 77", extra, f.Cell.I)
				}
			}
		})
	}
}
