package cstable

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestCSharpPointerTargetBlobKindRefused holds every C# read of the variable
// form to the blob-kind vectors (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused). The two reserved
// blob ids are types every reader names, whatever its root reaches: a declared
// slot naming a blob record of the other kind refuses the whole read, a blob
// record no pointer reaches is damage, and a blob record reached only from a
// field this reader does not declare is evolution, counted unknown. Each row
// of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv runs through the
// plain load, the region load, the retaining load and the builder load.
func TestCSharpPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	data, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := csharpPointerTargetRows(t, string(data))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	byUnit := map[string][]csharpPointerTargetRow{}
	var units []string
	for _, row := range rows {
		if _, ok := byUnit[row.unit]; !ok {
			units = append(units, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	var schema string
	for _, unit := range units {
		schema = csharpPointerTargetSchema(t, root, unit)
		using := csharpBlobUsing(t, schema)
		out := csBlobRun(t, schema, csharpBlobKindBody(t, dir, byUnit[unit]), using, nil)
		if strings.Contains(out, "FAIL ") || !strings.Contains(out, "PROBE OK") {
			t.Fatalf("the driven reader failed:\n%s", out)
		}
		checkCSharpBlobSaves(t, schema, out)
	}
	t.Run("negative control: the root closure decides which blobs a reader can name", func(t *testing.T) {
		const have = "if (!reached) { node.Declared = true; report.Unknown++; continue; }"
		const planted = "if (!reached) { report.Unknown++; continue; }"
		plantedOut := csBlobRun(t, schema, csharpBlobKindBody(t, dir, rows), csharpBlobUsing(t, schema), func(files map[string][]byte) {
			n := 0
			for name, src := range files {
				count := strings.Count(string(src), have)
				if count == 0 {
					continue
				}
				n += count
				files[name] = []byte(strings.Replace(string(src), have, planted, 1))
			}
			if n != 1 {
				t.Fatalf("the blob nameability anchor is not in the emitted reader exactly once")
			}
		})
		if !strings.Contains(plantedOut, "FAIL text-names-bytes") {
			t.Errorf("the planted root-closure rule did not turn text-names-bytes red:\n%s", plantedOut)
		}
	})
}

func csharpBlobUsing(t *testing.T, schema string) string {
	t.Helper()
	for line := range strings.SplitSeq(schema, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "package" {
			return "global using " + capitalize(f[1]) + ";"
		}
	}
	t.Fatal("schema names no package")
	return ""
}

func csharpBlobKindBody(t *testing.T, dir string, rows []csharpPointerTargetRow) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(csharpBlobKindHelpers)
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		slot := csharpBlobSlot(t, row.root)
		expect := "false"
		if row.verdict == "read" {
			expect = "true"
		}
		unknown := "false"
		unknownN := "0"
		if row.unknown >= 0 {
			unknown = "true"
			unknownN = fmt.Sprintf("%d", row.unknown)
		}
		checkNull := "false"
		wantNull := "false"
		if row.root == "Text" && row.verdict == "read" {
			checkNull = "true"
			if row.name == "text-unknown-field-bytes" {
				wantNull = "true"
			}
		}
		fmt.Fprintf(&b, csharpBlobKindCase,
			fmt.Sprintf("%x", wire), expect, unknown, unknownN, checkNull, wantNull, row.name,
			row.root, row.root, slot, slot,
			row.root, row.root, slot, slot,
			row.root, row.root, slot, slot,
			row.root, slot, slot,
			row.root)
	}
	b.WriteString(csharpBlobKindSaves)
	return b.String()
}

func csharpBlobSlot(t *testing.T, root string) string {
	t.Helper()
	switch root {
	case "Text":
		return "Body"
	case "Bin":
		return "Data"
	default:
		t.Fatalf("no blob slot for root %q", root)
		return ""
	}
}

const csharpBlobKindHelpers = `byte[] SaveValue(string label, long n, long w, byte[] buf)
{
 if (n <= 0 || w != n) { bad += ProbeLog.Fail(label + ": save " + w + " measure " + n); return null; }
 Console.WriteLine("SAVE " + label + " " + Convert.ToHexString(buf));
 return buf;
}
void RetainSaveText(byte[] wire, string label)
{
 long need = Schema.TextLoadMeasure(wire);
 if (need <= 0) { bad += ProbeLog.Fail(label + ": load measure " + need); return; }
 long cap = (need + 15L) & ~15L;
 unsafe {
  void* mem = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)cap, 16);
  try {
   TableRetain retain = default;
   var report = new TableReport();
   TextRow* p = Schema.TextLoadRetain((IntPtr)mem, cap, wire, ref retain, report);
   if (p == null || report.Refused || report.Damaged) { bad += ProbeLog.Fail(label + ": retain load " + report.Reason); return; }
   long m = Schema.TextMeasureRetain((IntPtr)mem, ref retain);
   if (m <= 0) { bad += ProbeLog.Fail(label + ": retain measure " + m); return; }
   byte[] buf = new byte[m];
   long w = Schema.TextSaveRetain((IntPtr)mem, ref retain, buf, report);
   SaveValue(label, m, w, buf);
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(mem); }
 }
}
void RetainSaveBin(byte[] wire, string label)
{
 long need = Schema.BinLoadMeasure(wire);
 if (need <= 0) { bad += ProbeLog.Fail(label + ": load measure " + need); return; }
 long cap = (need + 15L) & ~15L;
 unsafe {
  void* mem = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)cap, 16);
  try {
   TableRetain retain = default;
   var report = new TableReport();
   BinRow* p = Schema.BinLoadRetain((IntPtr)mem, cap, wire, ref retain, report);
   if (p == null || report.Refused || report.Damaged) { bad += ProbeLog.Fail(label + ": retain load " + report.Reason); return; }
   long m = Schema.BinMeasureRetain((IntPtr)mem, ref retain);
   if (m <= 0) { bad += ProbeLog.Fail(label + ": retain measure " + m); return; }
   byte[] buf = new byte[m];
   long w = Schema.BinSaveRetain((IntPtr)mem, ref retain, buf, report);
   SaveValue(label, m, w, buf);
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(mem); }
 }
}
`

const csharpBlobKindCase = `{
 byte[] wire = Convert.FromHexString(%q);
 bool expect = %s;
 bool checkUnknown = %s;
 int unknown = %s;
 bool checkNull = %s;
 bool wantNull = %s;
 string name = %q;
 var value = new %s();
 var report = new TableReport();
 var verdict = Schema.%sLoadVerdict(value, wire, report);
 bool ok = verdict == Schema.TableWire.Verdict.Ok && !report.Refused && !report.Damaged;
 if (ok != expect) { bad += ProbeLog.Fail(name + ": plain verdict " + verdict + " refused=" + report.Refused + " damaged=" + report.Damaged + " reason=" + report.Reason + " unknown=" + report.Unknown); }
 if (expect && checkUnknown && report.Unknown != unknown) { bad += ProbeLog.Fail(name + ": plain unknown " + report.Unknown); }
 if (expect && checkNull && ((value.%s == null) != wantNull)) { bad += ProbeLog.Fail(name + ": plain body null " + (value.%s == null)); }
 unsafe {
  long cap = 1L << 20;
  void* mem = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)cap, 16);
  try {
   var rr = new TableReport();
   %sRow* p = Schema.%sLoad((IntPtr)mem, cap, wire, rr);
   bool rok = p != null && !rr.Refused && !rr.Damaged;
   if (rok != expect) { bad += ProbeLog.Fail(name + ": region verdict refused=" + rr.Refused + " damaged=" + rr.Damaged + " reason=" + rr.Reason + " unknown=" + rr.Unknown); }
   if (expect && checkUnknown && rr.Unknown != unknown) { bad += ProbeLog.Fail(name + ": region unknown " + rr.Unknown); }
   if (expect && checkNull && p != null && ((p->%s == 0) != wantNull)) { bad += ProbeLog.Fail(name + ": region body null " + (p->%s == 0)); }
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(mem); }
 }
 unsafe {
  long cap = 1L << 20;
  void* mem = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)cap, 16);
  try {
   TableRetain retain = default;
   var tr = new TableReport();
   %sRow* tp = Schema.%sLoadRetain((IntPtr)mem, cap, wire, ref retain, tr);
   bool tok = tp != null && !tr.Refused && !tr.Damaged;
   if (tok != expect) { bad += ProbeLog.Fail(name + ": retaining verdict refused=" + tr.Refused + " damaged=" + tr.Damaged + " reason=" + tr.Reason + " unknown=" + tr.Unknown); }
   if (expect && checkUnknown && tr.Unknown != unknown) { bad += ProbeLog.Fail(name + ": retaining unknown " + tr.Unknown); }
   if (expect && checkNull && tp != null && ((tp->%s == 0) != wantNull)) { bad += ProbeLog.Fail(name + ": retaining body null " + (tp->%s == 0)); }
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(mem); }
 }
 unsafe { using (var builder = new %sBuilder()) {
  var br = new TableReport();
  bool bok = builder.Load(wire, br);
  if (bok != expect) { bad += ProbeLog.Fail(name + ": builder verdict " + bok + " refused=" + br.Refused + " damaged=" + br.Damaged + " reason=" + br.Reason + " unknown=" + br.Unknown); }
  if (expect && checkUnknown && br.Unknown != unknown) { bad += ProbeLog.Fail(name + ": builder unknown " + br.Unknown); }
  if (expect && checkNull && bok && ((builder.GetRoot()->%s == 0) != wantNull)) { bad += ProbeLog.Fail(name + ": builder body null " + (builder.GetRoot()->%s == 0)); }
 } }
 long measured = Schema.%sLoadMeasure(wire);
 if ((measured > 0) != expect) { bad += ProbeLog.Fail(name + ": measure " + measured); }
}
`

const csharpBlobKindSaves = `{
 var set = new Text();
 set.Body = new TableBlob(new byte[] { 104, 105 });
 long n = Schema.TextMeasure(set);
 byte[] buf = new byte[n > 0 ? n : 0];
 long w = n > 0 ? Schema.TextSave(set, buf) : 0;
 byte[] saved = SaveValue("text-set", n, w, buf);
 if (saved != null) { RetainSaveText(saved, "text-retain-set"); }
}
{
 var empty = new Text();
 long n = Schema.TextMeasure(empty);
 byte[] buf = new byte[n > 0 ? n : 0];
 long w = n > 0 ? Schema.TextSave(empty, buf) : 0;
 byte[] saved = SaveValue("text-null", n, w, buf);
 if (saved != null) { RetainSaveText(saved, "text-retain-null"); }
}
{
 var set = new Bin();
 set.Data = new TableBlob(new byte[] { 1, 2, 3 });
 long n = Schema.BinMeasure(set);
 byte[] buf = new byte[n > 0 ? n : 0];
 long w = n > 0 ? Schema.BinSave(set, buf) : 0;
 byte[] saved = SaveValue("bin-set", n, w, buf);
 if (saved != null) { RetainSaveBin(saved, "bin-retain-set"); }
}
{
 var empty = new Bin();
 long n = Schema.BinMeasure(empty);
 byte[] buf = new byte[n > 0 ? n : 0];
 long w = n > 0 ? Schema.BinSave(empty, buf) : 0;
 byte[] saved = SaveValue("bin-null", n, w, buf);
 if (saved != null) { RetainSaveBin(saved, "bin-retain-null"); }
}
`

func checkCSharpBlobSaves(t *testing.T, schema, out string) {
	t.Helper()
	want := []string{
		"text-set", "text-retain-set", "text-null", "text-retain-null",
		"bin-set", "bin-retain-set", "bin-null", "bin-retain-null",
	}
	seen := map[string]bool{}
	m := tabletext.NewModel(unitFrom(t, schema))
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "SAVE ") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			t.Errorf("save line %q", line)
			continue
		}
		label, hexText := f[1], f[2]
		seen[label] = true
		wire, err := hex.DecodeString(hexText)
		if err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		root := "Text"
		if strings.HasPrefix(label, "bin") {
			root = "Bin"
		}
		inst := m.New(m.Lookup(root))
		var rep tabletext.Report
		ok, decErr := tablewire.Decode(m, inst, wire, &rep)
		if !ok || decErr != nil || rep.Refused || rep.Unknown != 0 {
			t.Errorf("%s: engine ok=%v err=%v refused=%v unknown=%d reason=%q", label, ok, decErr, rep.Refused, rep.Unknown, rep.Reason)
		}
	}
	for _, label := range want {
		if !seen[label] {
			t.Errorf("missing save %s", label)
		}
	}
}

func csBlobRun(t *testing.T, schemaSrc, body, using string, edit func(map[string][]byte)) string {
	t.Helper()
	if edit == nil {
		return t08RunCS(t, schemaSrc, body, using)
	}
	slowtest.Gate(t, "an external toolchain")
	u := unitFrom(t, schemaSrc)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	edit(files)
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ProbeExtra0.cs"), []byte(using+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(fmt.Sprintf(t08Main, body)), 0o600); err != nil {
		t.Fatal(err)
	}
	return csBlobCompile(t, dir)
}

func csBlobCompile(t *testing.T, dir string) string {
	t.Helper()
	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	dp, err := exec.LookPath(dotnet)
	if err != nil {
		t.Fatalf("dotnet is not on PATH: %v", err)
	}
	rp, err := filepath.EvalSymlinks(dp)
	if err != nil {
		t.Fatalf("resolve dotnet %s: %v", dp, err)
	}
	root := filepath.Dir(rp)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	refs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refs) == 0 {
		t.Fatalf("no net10.0 reference pack under %s", root)
	}
	frameworks, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(frameworks) == 0 {
		t.Fatalf("no shared framework under %s", root)
	}
	version := filepath.Base(frameworks[0])
	sources, _ := filepath.Glob(filepath.Join(dir, "*.cs"))
	args := []string{"exec", cscs[0], "-nologo", "-unsafe", "-out:" + filepath.Join(dir, "probe.dll")}
	args = append(args, sources...)
	for _, r := range refs {
		dlls, _ := filepath.Glob(filepath.Join(r, "*.dll"))
		for _, dll := range dlls {
			args = append(args, "-r:"+dll)
		}
	}
	if out, err := testproc.Command(t, dotnet, args...).CombinedOutput(); err != nil {
		t.Fatalf("csc failed:\n%s", out)
	}
	rc := fmt.Sprintf(`{ "runtimeOptions": { "tfm": "net10.0", "framework": { "name": "Microsoft.NETCore.App", "version": %q } } }`, version)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := testproc.Command(t, dotnet, "exec", filepath.Join(dir, "probe.dll")).CombinedOutput()
	return string(out)
}
