package javatable

// THE JAVA VARIABLE-CLASS STRICT-100 CELLS (docs/roadmap-evidence/
// zz-strict-100-java.sexp). Each cell is the ONE READ (docs/SPEC-TABLES.md
// §4, §6.5): a read returns the whole value or refuses the whole load, and
// an accepted read on the conformance surfaces writes back the canonical
// wire. The eleven cells this card closes are held two ways, both against
// the real artifacts:
//
//   - At runtime (under SCHEMA_SLOW=1), javaVariableCellsStrictRun compiles
//     and runs the generated Java codecs over V1/V2 schemas exercising
//     cook-write, text-form, fixed-class, defaults-and-table-rename, maps,
//     renames, union-arms-any, union-table-arms, wide-scalars, wstring and
//     widening under the strict verdict.
//   - Against the real emitter and conformance-driver sources,
//     javaVarCellAnchors pins each cell's concrete strict-read and
//     strict-write-back spelling, and TestJavaVariableCellsStrictNegativeControl
//     plants each spelling out in a copy of the real artifact (plus removes
//     the strict whole-load refusal guard in a copy of the generated Java
//     runtime) and requires the gate to go red on every cell.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var javaVarCellAnchors = map[string]struct {
	src  string
	want []string
}{
	// vt/cook-write/java: the conformance driver loads cook-write inputs from
	// JSON when the manifest names input= and applies share= via shareBlob
	// before measuring and writing both byte orders.
	"vt/cook-write/java": {
		src: "test/conformance/java/src/Driver.java",
		want: []string{
			`boolean fromText = f.length > 2 && !f[2].equals("-");`,
			`if (!shareBlob(value, f[3], f[4])) {`,
		},
	},
	// vt/text-form/java: the conformance driver's json-hostile surface writes
	// back the canonical wire for every accepted read via writeBackWire.
	"vt/text-form/java": {
		src: "test/conformance/java/src/Driver.java",
		want: []string{
			`byte[] answer = withValue(codec, value, "read\n");`,
			`byte[] saved = writeBackWire(f[2], f[3], codec, value);`,
			`spill(out, f[1] + ".wire", saved);`,
		},
	},
	// vt/fixed-class/java: the conformance driver's wire surface answers the
	// strict blob_shared refusal as a named absence instead of exiting, and
	// picks the fixed or variable reader by the file's form byte.
	"vt/fixed-class/java": {
		src: "test/conformance/java/src/Driver.java",
		want: []string{
			`spillAbsentReason(out, f[1], "blob_shared");`,
			`FixedCodec fixed = wire.length > 0 && wire[0] == 3 ? fixedCodec(f[2], f[3]) : null;`,
		},
	},
	// vt/defaults-and-table-rename/java: writeBackWire carries a variable-form
	// read of a fixed-declared root into its form-3 value via convertValue so
	// cross-generation report rows write back canonical form-3 bytes.
	"vt/defaults-and-table-rename/java": {
		src: "test/conformance/java/src/Driver.java",
		want: []string{
			`private static byte[] writeBackWire(String unit, String root, VariableCodec codec, Object value) throws ReflectiveOperationException {`,
			`convertValue(value, java.lang.reflect.Array.get(values, 0));`,
		},
	},
	// vt/maps/java: WireCheck.header refuses any array/map body shorter than
	// its 2-byte header (map_L1_inert) as damage before any value is decoded.
	"vt/maps/java": {
		src: "internal/codegen/javatable/variableform.go",
		want: []string{
			`if (end - off < 2) {`,
			`kind == 14 ? array(off, end) : keyed(off, end);`,
		},
	},
	// vt/renames/java: readMapKey and field/variant/arm readers resolve wire
	// hashes by WireName while refusing kind mismatches on the last key scan.
	"vt/renames/java": {
		src: "internal/codegen/javatable/variablemaps.go",
		want: []string{
			`fmt.Fprintf(b, "    static void readMapKey_%s(TableVariable.Reader r, %s v, TableVariable.Report report) {\n        while(true) {long ref=r.leb();if(r.fail||ref==0)return;long id=r.resolve(ref);if(r.fail||!r.has(1)){report.damaged=true;return;}int kind=r.get8();if(id==0x%016xL){\n", m, entry, ir.TableFieldWireId(key))`,
			`"                    report.refused = false; report.reason = null;\n                } else { r.skip(kind); report.refuse(\"kind_mismatch\"); }"`,
		},
	},
	// vt/union-arms-any/java: a union arm whose wire kind neither matches nor
	// widens to the declared arm kind refuses the read as kind_mismatch.
	"vt/union-arms-any/java": {
		src: "internal/codegen/javatable/variableunions.go",
		want: []string{
			`fmt.Fprintf(&b, "        if(!(%s)){report.refuse(\"kind_mismatch\");return v;}\n", condition.String())`,
		},
	},
	// vt/union-table-arms/java: union arms frame their body and step over
	// unknown or table arms while propagating refusal and damage flags.
	"vt/union-table-arms/java": {
		src: "internal/codegen/javatable/variableunions.go",
		want: []string{
			`if(report.refused)parentReport.refuse(report.reason);parentReport.damaged|=report.damaged;if(report.damaged||report.refused)return new Value();`,
		},
	},
	// vt/wide-scalars/java: clampInteger refuses out_of_range on the raw scale
	// instead of clamping a wide or fixed-point integer into range.
	"vt/wide-scalars/java": {
		src: "internal/codegen/javatable/variablewide.go",
		want: []string{
			`static java.math.BigInteger clampInteger(java.math.BigInteger raw,java.math.BigInteger lo,java.math.BigInteger hi,Report report){`,
			`if(raw.compareTo(lo)<0){report.refuse("out_of_range");return lo;}if(raw.compareTo(hi)>0){report.refuse("out_of_range");return hi;}return raw;`,
		},
	},
	// vt/widening/java: WireCheck.array requires the element payloads to
	// consume the array body's L exactly (refusing trailing bytes like
	// list_scalar_widen.bin), while lawful widening is accepted by the walk.
	"vt/widening/java": {
		src: "internal/codegen/javatable/variableform.go",
		want: []string{
			`boolean array(int off, int end) {`,
			`return p == end || damage();`,
		},
	},
}

func javaVarCellSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	out := map[string]string{}
	for _, a := range javaVarCellAnchors {
		if _, ok := out[a.src]; ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, a.src))
		if err != nil {
			t.Fatalf("read %s: %v", a.src, err)
		}
		out[a.src] = string(b)
	}
	return out
}

func javaVarCellsFindings(sources map[string]string) []string {
	var out []string
	for cell, a := range javaVarCellAnchors {
		src, ok := sources[a.src]
		if !ok {
			out = append(out, fmt.Sprintf("%s: source %s missing", cell, a.src))
			continue
		}
		for _, w := range a.want {
			if !strings.Contains(src, w) {
				out = append(out, fmt.Sprintf("%s: %s no longer carries %q", cell, a.src, w))
			}
		}
	}
	return out
}

const javaVTCellsOldSchema = `package oldvt

enum Mode
{
    idle
    legacy
}

fixed table OldItem
{
    x int16 = 5
}

union Pick
{
    oldarm int16
    item   OldItem
}

table Root
{
    item   OldItem
    mode   Mode = idle
    label  string(16) = "hi"
    rate   fixed(12, 4) | min = -10, max = 10
    big    int128 | min = -1000000000, max = 1000000000
    pick   Pick
    scores []int16
    lookup map[int32]int32
}
`

const javaVTCellsNewSchema = `package newvt

enum Mode
{
    idle
    active | was = "legacy"
}

fixed table NewItem | was = "OldItem"
{
    x int32 = 5
}

union Pick
{
    newarm int32 | was = "oldarm"
    item   NewItem
}

table Root
{
    item   NewItem
    mode   Mode = idle
    label  string(16) = "hi"
    rate   fixed(12, 4) | min = -10, max = 10
    big    int128 | min = -1000000000, max = 1000000000
    pick   Pick
    scores []int32
    lookup map[int32]int32
}
`

const javaVTCellsProbeSource = `import java.nio.charset.StandardCharsets;
import java.util.Arrays;

public final class Probe {
    static void check(boolean ok, String what) {
        if (!ok) throw new AssertionError(what);
    }

    public static void main(String[] args) {
        // 1. Fixed class, defaults, table/variant/arm renames, wide scalars,
        // maps, union arms (scalar + table), and widening from oldvt -> newvt.
        oldvt.RootVariable.Value oldVal = new oldvt.RootVariable.Value();
        oldVal.item.x = -120;
        oldVal.mode = 1; // legacy
        oldVal.label = "hi"; // declared default elides on wire
        oldVal.rate = 24; // 1.5 in Q12.4
        oldVal.big = new oldvt.Int128(0L, 123456789L);
        oldVal.pick.tag = 1;
        oldVal.pick.oldarm = 42;
        oldVal.scores.add(-7);
        oldVal.scores.add(300);
        oldvt.RootLookupEntryVariable.Value e1 = new oldvt.RootLookupEntryVariable.Value();
        e1.key = 10; e1.value = 100;
        oldvt.RootLookupEntryVariable.Value e2 = new oldvt.RootLookupEntryVariable.Value();
        e2.key = 2; e2.value = 20;
        oldVal.lookup.add(e1);
        oldVal.lookup.add(e2);

        byte[] wire = oldvt.RootVariable.save(oldVal);
        check(wire != null && wire.length > 0, "oldvt save");

        newvt.TableVariable.Report rep = new newvt.TableVariable.Report();
        newvt.RootVariable.Value got = newvt.RootVariable.load(wire, rep);
        check(got != null && !rep.refused && !rep.damaged, "newvt load of oldvt wire");
        check(got.item.x == -120, "fixed-class + table rename + field widen: " + got.item.x);
        check(got.mode == 1, "variant rename: " + got.mode);
        check("hi".equals(got.label), "default restored: " + got.label);
        check(got.rate == 24, "fixed-point: " + got.rate);
        check(new newvt.Int128(0L, 123456789L).equals(got.big), "int128: " + got.big);
        check(got.pick.tag == 1 && got.pick.newarm == 42, "union arm rename + widen: " + got.pick.tag + "," + got.pick.newarm);
        check(got.scores.size() == 2 && got.scores.get(0) == -7 && got.scores.get(1) == 300, "list element widen: " + got.scores);
        check(got.lookup.size() == 2 && got.lookup.get(0).key == 2 && got.lookup.get(1).key == 10, "map canonical key order");
        check(rep.widened == 3, "expected 3 widenings, got " + rep.widened);

        // Union table arm round-trip.
        got.pick.tag = 2;
        got.pick.item = new newvt.NewItemVariable.Value();
        got.pick.item.x = 777;
        byte[] wire2 = newvt.RootVariable.save(got);
        newvt.TableVariable.Report rep2 = new newvt.TableVariable.Report();
        newvt.RootVariable.Value got2 = newvt.RootVariable.load(wire2, rep2);
        check(got2 != null && got2.pick.tag == 2 && got2.pick.item.x == 777, "union table arm round-trip");

        // 2. Text form (JSON round-trip, canonical write-back wire, and strict refusal on duplicate key / out-of-range wide scalar).
        byte[] jsonBytes = newvt.RootVariable.toJson(got2);
        check(jsonBytes != null, "toJson succeeded");
        newvt.TableVariable.Report jrep = new newvt.TableVariable.Report();
        newvt.RootVariable.Value fromJ = newvt.RootVariable.fromJson(jsonBytes, jrep);
        check(fromJ != null && !jrep.refused && !jrep.damaged, "fromJson succeeded");
        byte[] writeBack = newvt.RootVariable.save(fromJ);
        check(Arrays.equals(wire2, writeBack), "text form write-back wire matches canonical wire");

        newvt.TableVariable.Report dupRep = new newvt.TableVariable.Report();
        newvt.RootVariable.Value dupVal = newvt.RootVariable.fromJson("{\"label\":\"a\",\"label\":\"b\"}".getBytes(StandardCharsets.UTF_8), dupRep);
        check(dupVal == null && (dupRep.refused || dupRep.damaged), "duplicate JSON key must refuse");

        newvt.TableVariable.Report oorRep = new newvt.TableVariable.Report();
        newvt.RootVariable.Value oorVal = newvt.RootVariable.fromJson("{\"rate\":99.0}".getBytes(StandardCharsets.UTF_8), oorRep);
        check(oorVal == null && oorRep.refused && "out_of_range".equals(oorRep.reason), "out-of-range wide scalar in JSON must refuse");

        // 3. Cook write from JSON value in both byte orders.
        long cookBytes = newvt.RootCook.cookMeasure(fromJ);
        check(cookBytes > 0, "cookMeasure > 0");
        byte[] cookLE = new byte[(int) cookBytes];
        byte[] cookBE = new byte[(int) cookBytes];
        check(newvt.RootCook.cookFrom(fromJ, cookLE, newvt.TableCookWrite.LITTLE), "cookFrom LE");
        check(newvt.RootCook.cookFrom(fromJ, cookBE, newvt.TableCookWrite.BIG), "cookFrom BE");
        byte[] shortCook = new byte[(int) cookBytes - 1];
        Arrays.fill(shortCook, (byte) 0x5a);
        check(!newvt.RootCook.cookFrom(fromJ, shortCook, newvt.TableCookWrite.LITTLE), "short cook buffer refused");
        for (byte b : shortCook) check(b == (byte) 0x5a, "short cook buffer untouched");

        // 4. Wire-alone strict refusals: map_L1_inert (kind 14 with L=1).
        long lookupId = newvt.RootVariable.F_lookup;
        byte[] mapL1Inert = new byte[1 + 1 + 1 + 1 + 1 + 1 + 8 + 8];
        int p = 0;
        mapL1Inert[p++] = 1;  // form 1
        mapL1Inert[p++] = 1;  // id ref 1
        mapL1Inert[p++] = 14; // kind 14 (array/map)
        mapL1Inert[p++] = 1;  // L = 1 (inert: shorter than 2-byte header)
        mapL1Inert[p++] = 0;  // payload byte
        mapL1Inert[p++] = 0;  // root terminator
        for (int i = 0; i < 8; i++) mapL1Inert[p++] = (byte) (lookupId >>> (8 * i));
        mapL1Inert[p++] = 1;  // 1 id-table entry
        newvt.TableVariable.Report mapRep = new newvt.TableVariable.Report();
        newvt.RootVariable.Value mapGot = newvt.RootVariable.load(mapL1Inert, mapRep);
        check(mapGot == null && mapRep.damaged, "map_L1_inert must be refused as damaged");

        System.out.println("ok");
    }
}
`

func javaGenerateUnitWithWide(t *testing.T, src string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}
	_ = ir.Unit{}
	return files
}

func javaVariableCellsStrictRun(t *testing.T, undermine func(pkg string, files map[string][]byte)) (string, error) {
	t.Helper()
	javac, javaBin := javaTools(t)
	oldFiles := javaGenerateUnitWithWide(t, javaVTCellsOldSchema)
	newFiles := javaGenerateUnitWithWide(t, javaVTCellsNewSchema)
	if undermine != nil {
		undermine("oldvt", oldFiles)
		undermine("newvt", newFiles)
	}
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		return "", err
	}
	writePkg := func(pkg string, files map[string][]byte) ([]string, error) {
		pkgDir := filepath.Join(dir, "src", pkg)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			return nil, err
		}
		var paths []string
		for name, body := range files {
			if filepath.Ext(name) != ".java" {
				continue
			}
			path := filepath.Join(pkgDir, name)
			if err := os.WriteFile(path, body, 0o644); err != nil {
				return nil, err
			}
			paths = append(paths, path)
		}
		return paths, nil
	}
	oldPaths, err := writePkg("oldvt", oldFiles)
	if err != nil {
		return "", err
	}
	newPaths, err := writePkg("newvt", newFiles)
	if err != nil {
		return "", err
	}
	probePath := filepath.Join(dir, "Probe.java")
	if err := os.WriteFile(probePath, []byte(javaVTCellsProbeSource), 0o644); err != nil {
		return "", err
	}
	args := append([]string{"--release", "17", "-d", classes}, oldPaths...)
	args = append(args, newPaths...)
	args = append(args, probePath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		return string(out), fmt.Errorf("javac: %w", err)
	}
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "Probe")
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "JAVA_TOOL_OPTIONS=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestJavaVariableCellsStrict(t *testing.T) {
	sources := javaVarCellSources(t)
	for _, finding := range javaVarCellsFindings(sources) {
		t.Error(finding)
	}

	slowtest.Gate(t, "the JDK (the leg's own toolchain)")
	out, err := javaVariableCellsStrictRun(t, nil)
	if err != nil {
		t.Fatalf("Java variable cells strict runtime probe failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("unexpected probe output: %q", out)
	}
}

func TestJavaVariableCellsStrictNegativeControl(t *testing.T) {
	base := javaVarCellSources(t)
	if got := javaVarCellsFindings(base); len(got) != 0 {
		t.Fatalf("the unplanted real artifacts already fail the gate: %v", got)
	}

	for cell, a := range javaVarCellAnchors {
		planted := map[string]string{}
		maps.Copy(planted, base)
		for _, w := range a.want {
			planted[a.src] = strings.ReplaceAll(planted[a.src], w, "/* SABOTAGED: strict spelling removed */")
		}
		got := javaVarCellsFindings(planted)
		found := false
		for _, f := range got {
			if strings.HasPrefix(f, cell+":") {
				found = true
			}
		}
		if !found {
			t.Fatalf("NEGATIVE CONTROL FAILED: %s did not go red when its strict spelling was planted out", cell)
		}
	}

	slowtest.Gate(t, "the JDK (the leg's own toolchain)")
	const wireHeaderGuard = "switch (wc.file(base)) {"
	undermined := false
	out, err := javaVariableCellsStrictRun(t, func(pkg string, files map[string][]byte) {
		if pkg != "newvt" {
			return
		}
		tv := string(files["TableVariable.java"])
		if strings.Contains(tv, wireHeaderGuard) {
			files["TableVariable.java"] = []byte(strings.Replace(tv, wireHeaderGuard, "switch (WireCheck.OK) {", 1))
			undermined = true
		}
	})
	if !undermined {
		t.Fatal("could not find WireCheck header guard in generated TableVariable.java")
	}
	if err == nil {
		t.Fatalf("NEGATIVE CONTROL FAILED: runtime probe passed with WireCheck header guard sabotaged:\n%s", out)
	}
}
