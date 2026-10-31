package javatable

// fixedroadmap_t12 — the java leg's ordinary valid-data capability rows in
// docs/roadmap.sexp's node `fixed-tables` that waited on the registered Java
// target admitting union arms, union arrays and optional arrays
// (compiler/target_java.go):
//   - union-field-arms/java/valid-data
//   - payload-free-arms/java/valid-data
//   - union-arrays/java/valid-data
//   - optional-arrays/java/valid-data
//
// Law: docs/SPEC-TABLES.md §2.3 (optional wrapper), §2.6 (union arms and
// payload-free arms), §3.4 (the fixed form's record, constant arm storage,
// [N]T / [Min..Max]T, and optional wrapper) and docs/FIXED-FORM-ALGORITHM.md
// §1 (kind 32 void arm), §3 (prefill and union widest-arm slack), §4.6 (bounds).
//
// Every subtest generates through the REGISTERED Java target (`schema generate
// --lang java`), compiles the emitted Java surface beside a probe, writes
// non-default and boundary values, reads them back, and compares against the
// C++ reference bytes in build/fixedform-corpus where the corpus carries the
// construct (fu1.bin and fu2.bin).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	t12SchemaOnce sync.Once
	t12SchemaPath string
	t12SchemaErr  error
)

// t12SchemaBinary builds ./cmd/schema once so every subtest exercises the
// registered `schema generate --lang java` CLI path directly.
func t12SchemaBinary(t *testing.T) string {
	t.Helper()
	t12SchemaOnce.Do(func() {
		dir, err := os.MkdirTemp("", "schema-t12-bin-*")
		if err != nil {
			t12SchemaErr = err
			return
		}
		bin := filepath.Join(dir, "schema")
		cmd := exec.Command("go", "build", "-o", bin, "../../../cmd/schema")
		if out, err := cmd.CombinedOutput(); err != nil {
			t12SchemaErr = fmt.Errorf("go build cmd/schema: %w\n%s", err, out)
			return
		}
		t12SchemaPath = bin
	})
	if t12SchemaErr != nil {
		t.Fatal(t12SchemaErr)
	}
	return t12SchemaPath
}

// t12BuildRegistered writes schema files to a temporary directory, runs the
// registered `schema generate --lang java` CLI target into src/<pkg>, writes
// the probe class beside it, and compiles everything with javac --release 17.
func t12BuildRegistered(t *testing.T, pkg string, schemas map[string]string, probeClass, probeSrc string) string {
	t.Helper()
	javac, _ := javaTools(t)
	bin := t12SchemaBinary(t)
	dir := t.TempDir()
	schemaDir := filepath.Join(dir, "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range schemas {
		if err := os.WriteFile(filepath.Join(schemaDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srcPkg := filepath.Join(dir, "src", pkg)
	if err := os.MkdirAll(srcPkg, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "generate", "--lang", "java", "--out", srcPkg, schemaDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("schema generate --lang java: %v\n%s", err, out)
	}
	probePath := filepath.Join(dir, "src", probeClass+".java")
	if err := os.WriteFile(probePath, []byte(probeSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	genJava, err := filepath.Glob(filepath.Join(srcPkg, "*.java"))
	if err != nil || len(genJava) == 0 {
		t.Fatalf("no generated java files in %s: %v", srcPkg, err)
	}
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, genJava...)
	args = append(args, probePath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return classes
}

func t12RunJava(t *testing.T, classes, mainClass string, extraArgs ...string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	args := append([]string{"-ea", "-cp", classes, mainClass}, extraArgs...)
	out, err := exec.Command(javaBin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("java %s: %v\n%s", mainClass, err, out)
	}
	return string(out)
}

// t12UnionFieldArms proves union-field-arms/java/valid-data through the
// registered Java target (SPEC-TABLES §2.6, §3.4 constant arm storage;
// FIXED-FORM-ALGORITHM §3): direct scalar, text, bytes and fixed-array arms
// round-trip non-default and boundary values, and the C++ reference corpus
// files fu1.bin (FU1.schema) and fu2.bin (FU2.schema) match byte-for-byte on
// save and load cleanly on read.
func t12UnionFieldArms(t *testing.T) {
	const schema = `package probe

union Body
{
    scalar int32
    text   string(8)
    data   bytes(4)
    list   [3]int32
}

fixed table T
{
    lead int32 = 17
    body Body
    tail int32 = 19
}
`
	const probe = `import java.util.Arrays;

public final class ProbeUnionFieldArms {
    static void check(boolean ok, String why) {
        if (!ok) throw new AssertionError(why);
    }

    public static void main(String[] args) {
        probe.TableFixed.Entry[] plan = probe.TableFixed.plan(1 << 14);
        short[] pool = new short[1 << 14];

        // 1. Fresh value starts at None (ordinal 0) with declared neighbor defaults (§3.4).
        probe.TFixed.Value fresh = new probe.TFixed.Value();
        check(fresh.lead == 17 && fresh.tail == 19 && fresh.body.type == probe.BodyFixed.none,
              "fresh union must start at None with neighbor defaults");

        // 2. Direct scalar arm at int32 boundaries (Integer.MIN_VALUE, Integer.MAX_VALUE).
        for (int s : new int[]{Integer.MIN_VALUE, Integer.MAX_VALUE}) {
            probe.TFixed.Value[] in = { new probe.TFixed.Value() };
            in[0].lead = 101;
            in[0].tail = 102;
            in[0].body.type = probe.BodyFixed.scalar;
            in[0].body.scalar = s;
            byte[] buf = new byte[probe.TFixed.measure(1)];
            Arrays.fill(buf, (byte) 0xA5);
            check(probe.TFixed.save(in, 1, buf) == buf.length, "scalar save");
            probe.TFixed.Value[] out = { new probe.TFixed.Value() };
            probe.TableFixed.Report rep = new probe.TableFixed.Report();
            int n = probe.TFixed.load(out, 1, buf, plan, pool, probe.TFixed.image(), rep);
            check(n == 1 && !rep.refused && !rep.malformed && rep.clamped == 0, "scalar load");
            check(out[0].lead == 101 && out[0].tail == 102, "scalar neighbors");
            check(out[0].body.type == probe.BodyFixed.scalar && out[0].body.scalar == s, "scalar value");
        }

        // 3. Direct text arm at empty boundary (0) and full capacity (8).
        for (int len : new int[]{0, 8}) {
            probe.TFixed.Value[] in = { new probe.TFixed.Value() };
            in[0].lead = 201;
            in[0].tail = 202;
            in[0].body.type = probe.BodyFixed.text;
            in[0].body.textLength = len;
            for (int i = 0; i < len; i++) in[0].body.text[i] = (byte) ('a' + i);
            byte[] buf = new byte[probe.TFixed.measure(1)];
            check(probe.TFixed.save(in, 1, buf) == buf.length, "text save");
            probe.TFixed.Value[] out = { new probe.TFixed.Value() };
            probe.TableFixed.Report rep = new probe.TableFixed.Report();
            int n = probe.TFixed.load(out, 1, buf, plan, pool, probe.TFixed.image(), rep);
            check(n == 1 && !rep.refused && !rep.malformed && rep.clamped == 0, "text load");
            check(out[0].body.type == probe.BodyFixed.text && out[0].body.textLength == len, "text length");
            for (int i = 0; i < len; i++) check(out[0].body.text[i] == (byte) ('a' + i), "text byte");
            check(out[0].lead == 201 && out[0].tail == 202, "text neighbors");
        }

        // 4. Direct bytes arm at full capacity (4) with boundary byte values.
        {
            probe.TFixed.Value[] in = { new probe.TFixed.Value() };
            in[0].body.type = probe.BodyFixed.data;
            in[0].body.dataLength = 4;
            in[0].body.data[0] = 0x00;
            in[0].body.data[1] = (byte) 0xFF;
            in[0].body.data[2] = 0x7F;
            in[0].body.data[3] = (byte) 0x80;
            byte[] buf = new byte[probe.TFixed.measure(1)];
            check(probe.TFixed.save(in, 1, buf) == buf.length, "bytes save");
            probe.TFixed.Value[] out = { new probe.TFixed.Value() };
            probe.TableFixed.Report rep = new probe.TableFixed.Report();
            int n = probe.TFixed.load(out, 1, buf, plan, pool, probe.TFixed.image(), rep);
            check(n == 1 && !rep.refused && !rep.malformed && rep.clamped == 0, "bytes load");
            check(out[0].body.type == probe.BodyFixed.data && out[0].body.dataLength == 4, "bytes length");
            check(out[0].body.data[0] == 0x00 && out[0].body.data[1] == (byte) 0xFF &&
                  out[0].body.data[2] == 0x7F && out[0].body.data[3] == (byte) 0x80, "bytes payload");
        }

        // 5. Direct fixed-array arm [3]int32 with boundary elements.
        {
            probe.TFixed.Value[] in = { new probe.TFixed.Value() };
            in[0].body.type = probe.BodyFixed.list;
            in[0].body.list[0] = Integer.MIN_VALUE;
            in[0].body.list[1] = 0;
            in[0].body.list[2] = Integer.MAX_VALUE;
            byte[] buf = new byte[probe.TFixed.measure(1)];
            check(probe.TFixed.save(in, 1, buf) == buf.length, "array save");
            probe.TFixed.Value[] out = { new probe.TFixed.Value() };
            probe.TableFixed.Report rep = new probe.TableFixed.Report();
            int n = probe.TFixed.load(out, 1, buf, plan, pool, probe.TFixed.image(), rep);
            check(n == 1 && !rep.refused && !rep.malformed && rep.clamped == 0, "array load");
            check(out[0].body.type == probe.BodyFixed.list &&
                  out[0].body.list[0] == Integer.MIN_VALUE &&
                  out[0].body.list[1] == 0 &&
                  out[0].body.list[2] == Integer.MAX_VALUE, "array elements");
        }
        System.out.println("OK union-field-arms");
    }
}
`
	classes := t12BuildRegistered(t, "probe", map[string]string{"Probe.schema": schema}, "ProbeUnionFieldArms", probe)
	out := t12RunJava(t, classes, "ProbeUnionFieldArms")
	if !strings.Contains(out, "OK union-field-arms") {
		t.Fatalf("union-field-arms probe: got %q", out)
	}

	// Compare against the C++ reference bytes in build/fixedform-corpus (fu1.bin
	// and fu2.bin, written by test/tables/fixedform_dump.cpp fu1_file / fu2_file).
	corpus := corpusDir(t)
	fu1Bytes, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FU1.schema"))
	if err != nil {
		t.Fatal(err)
	}
	fu2Bytes, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FU2.schema"))
	if err != nil {
		t.Fatal(err)
	}
	const fu1Probe = `import java.nio.file.*;
import java.util.Arrays;

public final class ProbeFu1Oracle {
    static void check(boolean ok, String why) {
        if (!ok) throw new AssertionError(why);
    }

    public static void main(String[] args) throws Exception {
        byte[] ref = Files.readAllBytes(Path.of(args[0]));
        tblfu1.FuRootFixed.Value[] v = {
            new tblfu1.FuRootFixed.Value(),
            new tblfu1.FuRootFixed.Value(),
            new tblfu1.FuRootFixed.Value(),
        };
        v[0].flag = true;
        v[0].note = 44;
        v[0].notePresent = true;
        v[0].pick.type = tblfu1.PickFixed.labelled;
        v[0].pick.labelled.lead = 101;
        byte[] hello = "hello".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(hello, 0, v[0].pick.labelled.label, 0, hello.length);
        v[0].pick.labelled.labelLength = 5;
        v[0].pick.labelled.trail = 202;
        v[0].tail = 11;
        v[0].mark = (short) -1;
        v[0].heat = Float.intBitsToFloat(Integer.parseUnsignedInt(args[1], 16));

        v[1].flag = false;
        v[1].notePresent = false;
        v[1].note = 0;
        v[1].pick.type = tblfu1.PickFixed.plain;
        v[1].pick.plain.n = 303;
        v[1].tail = 12;
        v[1].mark = (short) -32768;
        v[1].heat = Float.intBitsToFloat(Integer.parseUnsignedInt(args[2], 16));

        v[2].flag = true;
        v[2].note = -7;
        v[2].notePresent = true;
        v[2].pick.type = tblfu1.PickFixed.labelled;
        v[2].pick.labelled.lead = -5;
        v[2].pick.labelled.labelLength = 0;
        v[2].pick.labelled.trail = 606;
        v[2].tail = 13;
        v[2].mark = (short) 32767;
        v[2].heat = Float.intBitsToFloat(Integer.parseUnsignedInt(args[3], 16));

        byte[] got = new byte[tblfu1.FuRootFixed.measure(3)];
        // THE MANIFEST'S heat VALUES ARE THE REFERENCE'S OWN. While the C++
        // dump still writes a NaN there (owed by tables-finite/cpp), the finite
        // rule (docs/SPEC-TABLES.md §3.3) makes the file a must-refuse vector:
        // the writer refuses the value and the reader refuses the file by name.
        if (args[4].equals("nonfinite")) {
            check(tblfu1.FuRootFixed.save(v, 3, got) == -1, "fu1: the writer refuses a non-finite heat");
            tblfu1.TableFixed.Report rep = new tblfu1.TableFixed.Report();
            int n = tblfu1.FuRootFixed.load(new tblfu1.FuRootFixed.Value[] {
                    new tblfu1.FuRootFixed.Value(), new tblfu1.FuRootFixed.Value(), new tblfu1.FuRootFixed.Value() },
                    3, ref, tblfu1.TableFixed.plan(1 << 14), new short[1 << 14], tblfu1.FuRootFixed.image(), rep);
            check(n == -1 && rep.refused && rep.reason == tblfu1.TableFixed.Reason.nonFiniteFloat
                    && "FuRoot.heat".equals(rep.nonFiniteFloat), "fu1: the reader refuses the non-finite heat by name");
            System.out.println("OK fu1.bin");
            return;
        }
        check(tblfu1.FuRootFixed.save(v, 3, got) == got.length, "fu1 save length");
        check(Arrays.equals(got, ref), "fu1 saved bytes must equal C++ reference fu1.bin");

        tblfu1.FuRootFixed.Value[] back = {
            new tblfu1.FuRootFixed.Value(),
            new tblfu1.FuRootFixed.Value(),
            new tblfu1.FuRootFixed.Value(),
        };
        tblfu1.TableFixed.Report rep = new tblfu1.TableFixed.Report();
        int n = tblfu1.FuRootFixed.load(back, 3, ref, tblfu1.TableFixed.plan(1 << 14), new short[1 << 14], tblfu1.FuRootFixed.image(), rep);
        check(n == 3 && !rep.refused && !rep.malformed, "fu1 load");
        check(back[0].pick.type == tblfu1.PickFixed.labelled &&
              back[0].pick.labelled.lead == 101 &&
              back[0].pick.labelled.labelLength == 5 &&
              back[0].pick.labelled.trail == 202 &&
              back[1].pick.type == tblfu1.PickFixed.plain &&
              back[1].pick.plain.n == 303 &&
              back[2].pick.type == tblfu1.PickFixed.labelled &&
              back[2].pick.labelled.lead == -5 &&
              back[2].pick.labelled.labelLength == 0 &&
              back[2].pick.labelled.trail == 606, "fu1 loaded values");
        System.out.println("OK fu1.bin");
    }
}
`
	fu1Classes := t12BuildRegistered(t, "tblfu1", map[string]string{"FU1.schema": string(fu1Bytes)}, "ProbeFu1Oracle", fu1Probe)
	rows := manifestRows(t, corpus)
	nonFinite := manifestNonFinite(t, corpus)
	spell := func(file string) string {
		if nonFinite[file] != "" {
			return "nonfinite"
		}
		return "finite"
	}
	fu1Out := t12RunJava(t, fu1Classes, "ProbeFu1Oracle", filepath.Join(corpus, "fu1.bin"),
		manifestBitsHex(t, rows, "fu1.bin", "r0.heat"), manifestBitsHex(t, rows, "fu1.bin", "r1.heat"),
		manifestBitsHex(t, rows, "fu1.bin", "r2.heat"), spell("fu1.bin"))
	if !strings.Contains(fu1Out, "OK fu1.bin") {
		t.Fatalf("fu1.bin oracle: got %q", fu1Out)
	}

	const fu2Probe = `import java.nio.file.*;
import java.util.Arrays;

public final class ProbeFu2Oracle {
    static void check(boolean ok, String why) {
        if (!ok) throw new AssertionError(why);
    }

    public static void main(String[] args) throws Exception {
        byte[] ref = Files.readAllBytes(Path.of(args[0]));
        tblfu2.FuRootFixed.Value[] v = {
            new tblfu2.FuRootFixed.Value(),
            new tblfu2.FuRootFixed.Value(),
        };
        v[0].flag = true;
        v[0].note = 55;
        v[0].notePresent = true;
        v[0].pick.type = tblfu2.PickFixed.labelled;
        v[0].pick.labelled.lead = 111;
        byte[] wide = "wide".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(wide, 0, v[0].pick.labelled.label, 0, wide.length);
        v[0].pick.labelled.labelLength = 4;
        v[0].pick.labelled.trail = 222;
        v[0].tail = 21;
        v[0].mark = -70000;
        v[0].heat = Double.longBitsToDouble(Long.parseUnsignedLong(args[1], 16));
        v[0].extra = 909;

        v[1].flag = false;
        v[1].notePresent = false;
        v[1].pick.type = tblfu2.PickFixed.plain;
        v[1].pick.plain.n = 404;
        v[1].tail = 22;
        v[1].mark = 70000;
        v[1].heat = Double.longBitsToDouble(Long.parseUnsignedLong(args[2], 16));
        v[1].extra = -11;

        byte[] got = new byte[tblfu2.FuRootFixed.measure(2)];
        if (args[3].equals("nonfinite")) {
            check(tblfu2.FuRootFixed.save(v, 2, got) == -1, "fu2: the writer refuses a non-finite heat");
            tblfu2.TableFixed.Report rep = new tblfu2.TableFixed.Report();
            int n = tblfu2.FuRootFixed.load(new tblfu2.FuRootFixed.Value[] {
                    new tblfu2.FuRootFixed.Value(), new tblfu2.FuRootFixed.Value() },
                    2, ref, tblfu2.TableFixed.plan(1 << 14), new short[1 << 14], tblfu2.FuRootFixed.image(), rep);
            check(n == -1 && rep.refused && rep.reason == tblfu2.TableFixed.Reason.nonFiniteFloat
                    && "FuRoot.heat".equals(rep.nonFiniteFloat), "fu2: the reader refuses the non-finite heat by name");
            System.out.println("OK fu2.bin");
            return;
        }
        check(tblfu2.FuRootFixed.save(v, 2, got) == got.length, "fu2 save length");
        check(Arrays.equals(got, ref), "fu2 saved bytes must equal C++ reference fu2.bin");
        System.out.println("OK fu2.bin");
    }
}
`
	fu2Classes := t12BuildRegistered(t, "tblfu2", map[string]string{"FU2.schema": string(fu2Bytes)}, "ProbeFu2Oracle", fu2Probe)
	fu2Out := t12RunJava(t, fu2Classes, "ProbeFu2Oracle", filepath.Join(corpus, "fu2.bin"),
		manifestBitsHex(t, rows, "fu2.bin", "r0.heat"), manifestBitsHex(t, rows, "fu2.bin", "r1.heat"), spell("fu2.bin"))
	if !strings.Contains(fu2Out, "OK fu2.bin") {
		t.Fatalf("fu2.bin oracle: got %q", fu2Out)
	}
}

// t12PayloadFreeArms proves payload-free-arms/java/valid-data through the
// registered Java target (SPEC-TABLES §2.6, §3.4; FIXED-FORM-ALGORITHM §1
// kind 32 and §3 widest-arm zero slack).
func t12PayloadFreeArms(t *testing.T) {
	const schema = `package probe

union Action
{
    ping
    value int32
    done
}

union Signal
{
    first
    last
}

fixed table Rec
{
    lead   int32
    action Action
    signal Signal
    tail   int32
}
`
	const probe = `import java.util.Arrays;

public final class ProbePayloadFreeArms {
    static void check(boolean ok, String why) {
        if (!ok) throw new AssertionError(why);
    }

    public static void main(String[] args) {
        probe.TableFixed.Entry[] plan = probe.TableFixed.plan(1 << 14);
        short[] pool = new short[1 << 14];
        probe.RecFixed.Value v = new probe.RecFixed.Value();
        v.lead = 17;
        v.tail = 19;
        for (int action : new int[]{probe.ActionFixed.none, probe.ActionFixed.ping, probe.ActionFixed.value, probe.ActionFixed.done}) {
            for (int signal : new int[]{probe.SignalFixed.first, probe.SignalFixed.last}) {
                v.action.type = (byte) action;
                v.action.value = 123456;
                v.signal.type = (byte) signal;
                byte[] bytes = new byte[probe.RecFixed.measure(1)];
                Arrays.fill(bytes, (byte) 0xA5);
                check(probe.RecFixed.save(new probe.RecFixed.Value[]{v}, 1, bytes) == bytes.length,
                      "payload-free save");
                int body = bytes.length - probe.RecFixed.bodyBytes;
                check((bytes[body + 4] & 0xFF) == action && (bytes[body + 9] & 0xFF) == signal,
                      "payload-free tags ride at declared offsets (§3.4)");
                if (action != probe.ActionFixed.value) {
                    for (int b = body + 5; b < body + 9; b++) {
                        check(bytes[b] == 0, "payload-free arm leaves zero widest-arm slack (§3)");
                    }
                }
                probe.RecFixed.Value[] out = { new probe.RecFixed.Value() };
                out[0].action.type = probe.ActionFixed.value;
                out[0].action.value = -1;
                probe.TableFixed.Report rep = new probe.TableFixed.Report();
                int n = probe.RecFixed.load(out, 1, bytes, plan, pool, probe.RecFixed.image(), rep);
                check(n == 1 && !rep.refused && !rep.malformed && rep.clamped == 0,
                      "payload-free load");
                check((out[0].action.type & 0xFF) == action && (out[0].signal.type & 0xFF) == signal,
                      "payload-free tags round-trip");
                if (action == probe.ActionFixed.value) {
                    check(out[0].action.value == 123456, "payload arm between free arms round-trips");
                }
                check(out[0].lead == 17 && out[0].tail == 19, "payload-free neighbors intact");
            }
        }
        System.out.println("OK payload-free-arms");
    }
}
`
	classes := t12BuildRegistered(t, "probe", map[string]string{"Probe.schema": schema}, "ProbePayloadFreeArms", probe)
	out := t12RunJava(t, classes, "ProbePayloadFreeArms")
	if !strings.Contains(out, "OK payload-free-arms") {
		t.Fatalf("payload-free-arms probe: got %q", out)
	}
}

// t12UnionArrays proves union-arrays/java/valid-data through the registered
// Java target (SPEC-TABLES §2.6, §3.4 [N]T / [Min..Max]T).
func t12UnionArrays(t *testing.T) {
	const schema = `package probe

type Ping
{
    n int32 = 7
}

union Reply
{
    ping  Ping
    code  int32
    empty
}

fixed table Rec
{
    lead    int32
    fixed2  [2]Reply
    history [..4]Reply
    trail   int32
}
`
	const probe = `public final class ProbeUnionArrays {
    static void check(boolean ok, String why) {
        if (!ok) throw new AssertionError(why);
    }

    public static void main(String[] args) {
        probe.TableFixed.Entry[] plan = probe.TableFixed.plan(1 << 14);
        short[] pool = new short[1 << 14];
        for (int count : new int[]{0, 4}) {
            probe.RecFixed.Value v = new probe.RecFixed.Value();
            v.lead = 3;
            v.trail = 5;
            v.fixed2[0].type = probe.ReplyFixed.code;
            v.fixed2[0].code = Integer.MIN_VALUE;
            v.fixed2[1].type = probe.ReplyFixed.empty;
            v.historyCount = count;
            for (int i = 0; i < count; i++) {
                if (i % 2 == 0) {
                    v.history[i].type = probe.ReplyFixed.ping;
                    v.history[i].ping.n = 11 * (i + 1);
                } else {
                    v.history[i].type = probe.ReplyFixed.code;
                    v.history[i].code = Integer.MAX_VALUE - i;
                }
            }
            byte[] bytes = new byte[probe.RecFixed.measure(1)];
            check(probe.RecFixed.save(new probe.RecFixed.Value[]{v}, 1, bytes) == bytes.length,
                  "union-arrays save");
            probe.RecFixed.Value[] out = { new probe.RecFixed.Value() };
            probe.TableFixed.Report rep = new probe.TableFixed.Report();
            int n = probe.RecFixed.load(out, 1, bytes, plan, pool, probe.RecFixed.image(), rep);
            check(n == 1 && !rep.refused && !rep.malformed && rep.clamped == 0,
                  "union-arrays load");
            check(out[0].lead == 3 && out[0].trail == 5 && out[0].historyCount == count,
                  "union-arrays count and neighbors");
            check(out[0].fixed2[0].type == probe.ReplyFixed.code &&
                  out[0].fixed2[0].code == Integer.MIN_VALUE &&
                  out[0].fixed2[1].type == probe.ReplyFixed.empty,
                  "union-arrays fixed2 elements");
            for (int i = 0; i < count; i++) {
                if (i % 2 == 0) {
                    check(out[0].history[i].type == probe.ReplyFixed.ping &&
                          out[0].history[i].ping.n == 11 * (i + 1),
                          "union-arrays even element " + i);
                } else {
                    check(out[0].history[i].type == probe.ReplyFixed.code &&
                          out[0].history[i].code == Integer.MAX_VALUE - i,
                          "union-arrays odd element " + i);
                }
            }
        }
        System.out.println("OK union-arrays");
    }
}
`
	classes := t12BuildRegistered(t, "probe", map[string]string{"Probe.schema": schema}, "ProbeUnionArrays", probe)
	out := t12RunJava(t, classes, "ProbeUnionArrays")
	if !strings.Contains(out, "OK union-arrays") {
		t.Fatalf("union-arrays probe: got %q", out)
	}
}

// t12OptionalArrays proves optional-arrays/java/valid-data through the
// registered Java target (SPEC-TABLES §2.3, §3.4 optional wrapper).
func t12OptionalArrays(t *testing.T) {
	const schema = `package probe

fixed table T
{
    lead  int32
    xs    ?[..4]int32
    ys    ?[4]int32
    trail int32
}
`
	probe := t09SaveLoadProbe("ProbeOptionalArrays", "probe", "T", 4, `
        // record 0: both PRESENT, xs at capacity 4 with int32 bounds, ys at 4
        vals[0].xsPresent = true;
        vals[0].xsCount = 4;
        vals[0].xs[0] = Integer.MIN_VALUE;
        vals[0].xs[1] = Integer.MAX_VALUE;
        vals[0].xs[2] = -1;
        vals[0].xs[3] = 0;
        vals[0].ysPresent = true;
        vals[0].ys[0] = 7; vals[0].ys[1] = -8;
        vals[0].ys[2] = Integer.MAX_VALUE; vals[0].ys[3] = Integer.MIN_VALUE;
        vals[0].lead = 101; vals[0].trail = 102;
        // record 1: PRESENT and EMPTY — distinct from absent (§2.3, §3.4)
        vals[1].xsPresent = true;
        vals[1].xsCount = 0;
        vals[1].lead = 201; vals[1].trail = 202;
        // record 2: ABSENT, with nonzero caller storage that must not ride
        vals[2].xsPresent = false;
        vals[2].xsCount = 4;
        vals[2].xs[0] = 91; vals[2].xs[1] = 92; vals[2].xs[2] = 93; vals[2].xs[3] = 94;
        vals[2].ysPresent = false;
        vals[2].ys[0] = 55; vals[2].ys[1] = 56; vals[2].ys[2] = 57; vals[2].ys[3] = 58;
        vals[2].lead = 301; vals[2].trail = 302;
        // record 3: PRESENT all-default fixed array — still rides present
        vals[3].ysPresent = true;
        vals[3].lead = 401; vals[3].trail = 402;`)
	classes := t12BuildRegistered(t, "probe", map[string]string{"Probe.schema": schema}, "ProbeOptionalArrays", probe)
	r := parseProbe(t, t12RunJava(t, classes, "ProbeOptionalArrays"))
	t09RequireClean(t, "optional-arrays", r, 4)

	for i := range 4 {
		if got := r.fresh[fmt.Sprintf("f%d.xsPresent", i)]; got != "false" {
			t.Errorf("optional-arrays: fresh xsPresent = %s, want false", got)
		}
	}
	t09Want(t, "optional-arrays r0", r, "v0.xsPresent", "true")
	t09Want(t, "optional-arrays r0", r, "v0.xsCount", "4")
	t09Want(t, "optional-arrays r0", r, "v0.xs", "list:-2147483648,2147483647,-1,0")
	t09Want(t, "optional-arrays r0", r, "v0.ysPresent", "true")
	t09Want(t, "optional-arrays r0", r, "v0.ys", "list:7,-8,2147483647,-2147483648")
	t09Want(t, "optional-arrays r0", r, "v0.lead", "101")
	t09Want(t, "optional-arrays r0", r, "v0.trail", "102")
	t09Want(t, "optional-arrays r1", r, "v1.xsPresent", "true")
	t09Want(t, "optional-arrays r1", r, "v1.xsCount", "0")
	t09Want(t, "optional-arrays r2", r, "v2.xsPresent", "false")
	t09Want(t, "optional-arrays r2", r, "v2.xsCount", "0")
	t09Want(t, "optional-arrays r2", r, "v2.xs", "list:0,0,0,0")
	t09Want(t, "optional-arrays r2", r, "v2.ysPresent", "false")
	t09Want(t, "optional-arrays r2", r, "v2.ys", "list:0,0,0,0")
	t09Want(t, "optional-arrays r2", r, "v2.lead", "301")
	t09Want(t, "optional-arrays r3", r, "v3.ysPresent", "true")
	t09Want(t, "optional-arrays r3", r, "v3.ys", "list:0,0,0,0")
}

func TestFixedRoadmapJavaT12BatchesUnions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"union-field-arms/java/valid-data", t12UnionFieldArms},
		{"payload-free-arms/java/valid-data", t12PayloadFreeArms},
		{"union-arrays/java/valid-data", t12UnionArrays},
		{"optional-arrays/java/valid-data", t12OptionalArrays},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}
