package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func compileAndRunJavaProbe(t *testing.T, schema, pkg, driver string) (map[string][]byte, string) {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatalf("java.Generate: %v", err)
	}
	for name, data := range pkt {
		if _, ok := files[name]; !ok && strings.HasSuffix(name, ".java") {
			files[name] = data
		}
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, pkg)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	if driver != "" {
		mainPath := filepath.Join(dir, "Main.java")
		if err := os.WriteFile(mainPath, []byte(driver), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, mainPath)
	}
	classesDir := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-encoding", "UTF-8", "-d", classesDir}, sources...)
	if out, err := exec.Command("javac", args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if driver == "" {
		return files, ""
	}
	out, err := exec.Command("java", "-ea", "-cp", classesDir, "Main").CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	return files, strings.TrimSpace(string(out))
}

func TestJavaOneSidedRange(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	const schema = `package probe

fixed table ClampRow
{
    loOnly int32 = 10 | min = 10
    hiOnly int32 = 0 | max = 50
    u64Lo uint64 = 100 | min = 100
    u8Hi uint8 = 0 | max = 200
}
`
	const driver = `import probe.*;
import java.nio.charset.StandardCharsets;

public final class Main {
    private static void check(boolean ok, String msg) {
        if (!ok) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        // 1. Variable wire + JSON clamping on one-sided bounds (including uint64 | min = 100)
        TableVariable.Report jrep = new TableVariable.Report();
        ClampRowVariable.Value jv = ClampRowVariable.fromJson(
            "{\"loOnly\":5,\"hiOnly\":99,\"u64Lo\":42,\"u8Hi\":250}".getBytes(StandardCharsets.UTF_8),
            jrep);
        check(jv != null && !jrep.malformed, "fromJson malformed");
        check(jv.loOnly == 10, "loOnly clamp: " + jv.loOnly);
        check(jv.hiOnly == 50, "hiOnly clamp: " + jv.hiOnly);
        check(jv.u64Lo == 100L, "u64Lo clamp: " + jv.u64Lo);
        check(jv.u8Hi == 200, "u8Hi clamp: " + jv.u8Hi);
        check(jrep.clamped == 4, "expected 4 JSON clamps, got " + jrep.clamped);

        // Verify uint64 near 2^64-1 is NOT clamped when only min = 100 is declared
        TableVariable.Report jrep2 = new TableVariable.Report();
        ClampRowVariable.Value jv2 = ClampRowVariable.fromJson(
            "{\"loOnly\":1000,\"hiOnly\":-1000,\"u64Lo\":18446744073709551615,\"u8Hi\":0}".getBytes(StandardCharsets.UTF_8),
            jrep2);
        check(jv2 != null && !jrep2.malformed, "fromJson2 malformed");
        check(jv2.loOnly == 1000 && jv2.hiOnly == -1000 && jv2.u64Lo == -1L && jv2.u8Hi == 0, "unclamped ends");
        check(jrep2.clamped == 0, "expected 0 clamps on open ends, got " + jrep2.clamped);

        // 2. Fixed form load clamping on one-sided bounds (including uint64 | min = 100)
        ClampRowFixed.Value fv = new ClampRowFixed.Value();
        fv.loOnly = 3;
        fv.hiOnly = 77;
        fv.u64Lo = 50L;
        fv.u8Hi = (byte) 240;
        byte[] fbuf = new byte[ClampRowFixed.bodyBytes];
        ClampRowFixed.writeBody(fbuf, 0, fv);
        TableFixed.Report frep = new TableFixed.Report();
        ClampRowFixed.Value floaded = new ClampRowFixed.Value();
        ClampRowFixed.scatter(fbuf, 0, floaded, frep);
        check(floaded.loOnly == 10, "Fixed loOnly: " + floaded.loOnly);
        check(floaded.hiOnly == 50, "Fixed hiOnly: " + floaded.hiOnly);
        check(floaded.u64Lo == 100L, "Fixed u64Lo: " + floaded.u64Lo);
        check((floaded.u8Hi & 0xff) == 200, "Fixed u8Hi: " + (floaded.u8Hi & 0xff));
        check(frep.clamped == 4, "Fixed clamped: " + frep.clamped);

        // High uint64 (0xffffffffffffffffL) on Fixed form must not clamp when only min = 100 is declared
        fv.loOnly = 10;
        fv.hiOnly = 50;
        fv.u64Lo = -1L;
        fv.u8Hi = (byte) 200;
        ClampRowFixed.writeBody(fbuf, 0, fv);
        TableFixed.Report frep2 = new TableFixed.Report();
        ClampRowFixed.scatter(fbuf, 0, floaded, frep2);
        check(floaded.u64Lo == -1L && frep2.clamped == 0, "Fixed high u64 clamped: " + frep2.clamped);
    }
}
`
	compileAndRunJavaProbe(t, schema, "probe", driver)
}

func TestJavaDeclaredForms(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()

	for _, dec := range []string{
		"wire",
		"wire,json",
		"wire,cook",
		"wire,block",
		"wire,json,cook",
		"wire,json,block",
		"wire,cook,block",
		"wire,json,cook,block",
	} {
		t.Run("variable/"+dec, func(t *testing.T) {
			t.Parallel()
			schema := `package probe
enum Key { A, B }
union Choice {
    a int32
    b int64
}
table Pong | forms = "` + dec + `"
{
    x int32
    m map[string(8)]int32
    k [Key]int32
    c Choice
}
`
			files, _ := compileAndRunJavaProbe(t, schema, "probe", "")
			wantJson := strings.Contains(dec, "json")
			wantCook := strings.Contains(dec, "cook")
			if _, ok := files["TableJson.java"]; ok != wantJson {
				t.Errorf("TableJson.java present=%v, want %v", ok, wantJson)
			}
			if _, ok := files["PongCook.java"]; ok != wantCook {
				t.Errorf("PongCook.java present=%v, want %v", ok, wantCook)
			}
			if _, ok := files["PongMEntryCook.java"]; ok != wantCook {
				t.Errorf("PongMEntryCook.java present=%v, want %v", ok, wantCook)
			}
			varSrc := string(files["PongVariable.java"])
			if strings.Contains(varSrc, "public static byte[] toJson(") != wantJson {
				t.Errorf("PongVariable.toJson present != %v", wantJson)
			}
		})
		t.Run("fixed/"+dec, func(t *testing.T) {
			t.Parallel()
			schema := `package probe
fixed table Fox | forms = "` + dec + `"
{
    x int32
}
`
			files, _ := compileAndRunJavaProbe(t, schema, "probe", "")
			wantJson := strings.Contains(dec, "json")
			wantCook := strings.Contains(dec, "cook")
			wantBlock := strings.Contains(dec, "block")
			if _, ok := files["TableJson.java"]; ok != wantJson {
				t.Errorf("TableJson.java present=%v, want %v", ok, wantJson)
			}
			if _, ok := files["FoxCook.java"]; ok != wantCook {
				t.Errorf("FoxCook.java present=%v, want %v", ok, wantCook)
			}
			if _, ok := files["FoxBlock.java"]; ok != wantBlock {
				t.Errorf("FoxBlock.java present=%v, want %v", ok, wantBlock)
			}
			if _, ok := files["TableBytes.java"]; ok != (wantCook || wantBlock) {
				t.Errorf("TableBytes.java present=%v, want %v", ok, wantCook || wantBlock)
			}
		})
	}

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		schema := `package probe
table Pong | forms = "wire"
{
    x int32
    m map[string(8)]int32
}
table Ping
{
    x int32
}
fixed table FoxWire | forms = "wire"
{
    x int32
}
fixed table FoxAll
{
    x int32
}
`
		files, _ := compileAndRunJavaProbe(t, schema, "probe", "")
		if _, ok := files["PongCook.java"]; ok {
			t.Errorf("PongCook.java emitted for wire-only Pong")
		}
		if _, ok := files["FoxWireCook.java"]; ok {
			t.Errorf("FoxWireCook.java emitted for wire-only FoxWire")
		}
		if _, ok := files["FoxWireBlock.java"]; ok {
			t.Errorf("FoxWireBlock.java emitted for wire-only FoxWire")
		}
		if _, ok := files["PingCook.java"]; !ok {
			t.Errorf("PingCook.java missing for undeclared Ping")
		}
		if _, ok := files["FoxAllCook.java"]; !ok {
			t.Errorf("FoxAllCook.java missing for undeclared FoxAll")
		}
		if _, ok := files["FoxAllBlock.java"]; !ok {
			t.Errorf("FoxAllBlock.java missing for undeclared FoxAll")
		}
		if strings.Contains(string(files["PongVariable.java"]), "public static byte[] toJson(") {
			t.Errorf("PongVariable.toJson emitted for wire-only Pong")
		}
		if !strings.Contains(string(files["PingVariable.java"]), "public static byte[] toJson(") {
			t.Errorf("PingVariable.toJson missing for undeclared Ping")
		}
	})
}

func TestJavaJsonArrayRoot(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	const schema = `package probe

table GoalList | json = "array"
{
    goals [..8]int32
}

table Holder
{
    g GoalList
    gs [..4]GoalList
}
`
	const driver = `import probe.*;
import java.nio.charset.StandardCharsets;

public final class Main {
    private static void check(boolean ok, String msg) {
        if (!ok) throw new AssertionError(msg);
    }

    public static void main(String[] args) {
        // 1. Array root write
        GoalListVariable.Value v = new GoalListVariable.Value();
        v.goals.add(10);
        v.goals.add(20);
        String json = new String(GoalListVariable.toJson(v), StandardCharsets.UTF_8);
        check("[\n  10,\n  20\n]\n".equals(json), "unexpected GoalList json:\n" + json);

        // 2. Array root read
        TableVariable.Report r1 = new TableVariable.Report();
        GoalListVariable.Value g = GoalListVariable.fromJson("[10, 20]".getBytes(StandardCharsets.UTF_8), r1);
        check(g != null && !r1.malformed, "fromJson array failed");
        check(g.goals.size() == 2 && g.goals.get(0) == 10 && g.goals.get(1) == 20, "goals mismatch");

        // 3. Object at array root refused by name
        TableVariable.Report r2 = new TableVariable.Report();
        GoalListVariable.Value bad = GoalListVariable.fromJson("{\"goals\":[10]}".getBytes(StandardCharsets.UTF_8), r2);
        check(bad == null && r2.malformed, "expected malformed on object at array root");
        check("json_root_not_array: GoalList".equals(r2.reason), "unexpected reason: " + r2.reason);

        // 4. Nested by-value field and array of array-root tables
        TableVariable.Report r3 = new TableVariable.Report();
        HolderVariable.Value h = HolderVariable.fromJson(
            "{\"g\":[7,8],\"gs\":[[1,2],[3]]}".getBytes(StandardCharsets.UTF_8), r3);
        check(h != null && !r3.malformed && r3.kindMismatch == 0, "nested fromJson failed");
        check(h.g.goals.size() == 2 && h.g.goals.get(0) == 7 && h.g.goals.get(1) == 8, "h.g mismatch");
        check(h.gs.size() == 2 && h.gs.get(0).goals.size() == 2 && h.gs.get(1).goals.get(0) == 3, "h.gs mismatch");
        String hjson = new String(HolderVariable.toJson(h), StandardCharsets.UTF_8);
        String wantH = "{\n  \"g\": [\n    7,\n    8\n  ],\n  \"gs\": [\n    [\n      1,\n      2\n    ],\n    [\n      3\n    ]\n  ]\n}\n";
        check(wantH.equals(hjson), "unexpected Holder json:\n" + hjson);

        // 5. Object where nested array-root is: ordinary kindMismatch
        TableVariable.Report r4 = new TableVariable.Report();
        HolderVariable.Value h2 = HolderVariable.fromJson(
            "{\"g\":{\"goals\":[1]},\"gs\":[{\"goals\":[2]}]}".getBytes(StandardCharsets.UTF_8), r4);
        check(h2 != null && !r4.malformed && r4.kindMismatch == 2, "expected 2 kindMismatch, got " + r4.kindMismatch);
    }
}
`
	compileAndRunJavaProbe(t, schema, "probe", driver)
}
