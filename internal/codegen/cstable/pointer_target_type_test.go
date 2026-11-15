// TestCSharpPointerTargetTypeChecked pins docs/SPEC-TABLES.md §3.1's rule for a
// pointer's target type (docs/rulings.json `pointer-target-type-checked`): in
// the variable form, a pointer slot that names a record of a type this reader
// DECLARES but that is not the type the slot requires is damage and refuses the
// whole load. A record whose type this reader does not declare (a newer
// writer's type) stays lawful: the load reads, the pointer is null, and the
// record is counted unknown once. The vectors under
// testdata/wire/pointer-target-type/ are the cases; every read entry the C#
// leg answers a verdict from holds them.
package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCSharpPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := csharpPointerTargetRows(t, string(data))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	schema := csharpPointerTargetSchema(t, root, rows[0].unit)
	for _, row := range rows {
		if row.unit != rows[0].unit {
			t.Fatalf("MANIFEST.tsv names more than one unit (%q and %q)", rows[0].unit, row.unit)
		}
	}

	var body strings.Builder
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		expect := row.verdict == "read"
		read := "false"
		if expect {
			read = "true"
		}
		unknown := ""
		if row.unknown >= 0 {
			unknown = fmt.Sprintf("if (report.Unknown != %d) { bad += ProbeLog.Fail(%q + \": plain unknown \" + report.Unknown); }\n", row.unknown, row.name)
		}
		pointer := ""
		nullCheck := "false"
		if row.name == "next-writer-only" {
			pointer = fmt.Sprintf(`if (value.Next != null) { bad += ProbeLog.Fail(%q + ": plain pointer is not null"); }
`, row.name)
			nullCheck = "true"
		}
		fmt.Fprintf(&body, `{
 byte[] wire = Convert.FromHexString(%q);
 bool expect = %s;
 var value = new Node();
 var report = new TableReport();
 var verdict = Schema.NodeLoadVerdict(value, wire, report);
 bool ok = verdict == Schema.TableWire.Verdict.Ok;
 if (ok != expect) { bad += ProbeLog.Fail(%q + ": plain verdict " + verdict + " refused=" + report.Refused + " damaged=" + report.Damaged); }
 if (expect && (report.Refused || report.Damaged)) { bad += ProbeLog.Fail(%q + ": plain read is not clean"); }
 if (!expect && !(report.Refused || report.Damaged)) { bad += ProbeLog.Fail(%q + ": plain refusal is not a verdict"); }
%s%s
 unsafe {
  long cap = 1 << 16;
  void* mem = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)cap, 16);
  try {
   var rr = new TableReport();
   NodeRow* p = Schema.NodeLoad((IntPtr)mem, cap, wire, rr);
   bool rok = p != null;
   if (rok != expect) { bad += ProbeLog.Fail(%q + ": region verdict " + rok + " refused=" + rr.Refused + " damaged=" + rr.Damaged); }
   if (expect && (rr.Refused || rr.Damaged)) { bad += ProbeLog.Fail(%q + ": region read is not clean"); }
   if (!expect && !(rr.Refused || rr.Damaged)) { bad += ProbeLog.Fail(%q + ": region refusal is not a verdict"); }
   if (expect && rr.Unknown != report.Unknown) { bad += ProbeLog.Fail(%q + ": region unknown " + rr.Unknown); }
   if (%s && p->Next != 0) { bad += ProbeLog.Fail(%q + ": region pointer is not null"); }
   TableRetain retain = default;
   var tr = new TableReport();
   NodeRow* tp = Schema.NodeLoadRetain((IntPtr)mem, cap, wire, ref retain, tr);
   bool tok = tp != null;
   if (tok != expect) { bad += ProbeLog.Fail(%q + ": retaining verdict " + tok + " refused=" + tr.Refused + " damaged=" + tr.Damaged); }
   if (expect && (tr.Refused || tr.Damaged)) { bad += ProbeLog.Fail(%q + ": retaining read is not clean"); }
   if (!expect && !(tr.Refused || tr.Damaged)) { bad += ProbeLog.Fail(%q + ": retaining refusal is not a verdict"); }
   if (expect && tr.Unknown != report.Unknown) { bad += ProbeLog.Fail(%q + ": retaining unknown " + tr.Unknown); }
   if (%s && tp->Next != 0) { bad += ProbeLog.Fail(%q + ": retaining pointer is not null"); }
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(mem); }
 }
 unsafe { using (var builder = new NodeBuilder()) {
  var br = new TableReport();
  bool bok = builder.Load(wire, br);
  if (bok != expect) { bad += ProbeLog.Fail(%q + ": builder verdict " + bok + " refused=" + br.Refused + " damaged=" + br.Damaged); }
  if (expect && (br.Refused || br.Damaged)) { bad += ProbeLog.Fail(%q + ": builder read is not clean"); }
  if (!expect && !(br.Refused || br.Damaged)) { bad += ProbeLog.Fail(%q + ": builder refusal is not a verdict"); }
  if (expect && br.Unknown != report.Unknown) { bad += ProbeLog.Fail(%q + ": builder unknown " + br.Unknown); }
  if (%s && builder.GetRoot()->Next != 0) { bad += ProbeLog.Fail(%q + ": builder pointer is not null"); }
 } }
 long measured = Schema.NodeLoadMeasure(wire);
 if ((measured > 0) != expect) { bad += ProbeLog.Fail(%q + ": untyped measure " + measured); }
 TableRefuseReason why;
 long typed = Schema.NodeLoadMeasure(wire, out why);
 if ((typed > 0) != expect) { bad += ProbeLog.Fail(%q + ": typed measure " + typed + " reason=" + why); }
 if (!expect && why == TableRefuseReason.ok) { bad += ProbeLog.Fail(%q + ": typed measure names no refusal"); }
 long four = Schema.NodeLoadMeasure(wire, out long dataBytes, out long attributionBytes, out TableRefuseReason why4);
 if ((four > 0) != expect) { bad += ProbeLog.Fail(%q + ": four-out measure " + four + " reason=" + why4); }
 if (!expect && why4 == TableRefuseReason.ok) { bad += ProbeLog.Fail(%q + ": four-out measure names no refusal"); }
 if (expect && (four != measured || dataBytes <= 0 || attributionBytes <= 0)) { bad += ProbeLog.Fail(%q + ": four-out measure disagrees " + four + "/" + measured + " data=" + dataBytes + " attribution=" + attributionBytes); }
}
`, fmt.Sprintf("%x", wire), read, row.name, row.name, row.name, unknown, pointer,
			row.name, row.name, row.name, row.name, nullCheck, row.name,
			row.name, row.name, row.name, row.name, nullCheck, row.name,
			row.name, row.name, row.name, row.name, nullCheck, row.name,
			row.name, row.name, row.name, row.name, row.name, row.name)
	}
	t08CSOK(t, schema, body.String(), "global using Pointertargettype;")
}

type csharpPointerTargetRow struct {
	name    string
	unit    string
	root    string
	file    string
	verdict string
	unknown int
}

func csharpPointerTargetRows(t *testing.T, manifest string) []csharpPointerTargetRow {
	t.Helper()
	var rows []csharpPointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := csharpPointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

func csharpPointerTargetSchema(t *testing.T, root, unit string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(unit))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s: %v", unit, err)
	}
	var schema string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if schema != "" {
			t.Fatalf("%s declares more than one schema file", unit)
		}
		schema = string(data)
	}
	if schema == "" {
		t.Fatalf("%s declares no schema file", unit)
	}
	return schema
}
