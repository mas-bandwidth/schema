package javatable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedClassOnTableWireJava holds the three roadmap cells of this card:
//   - vt/fixed-class/java: a closure holding a fixed table emits form 1
//     (TableVariable.java, TableJson.java, <Type>Variable.java) and round-trips
//     its instances on form 1 and text byte for byte against the Go leg.
//   - vt/defaults-and-table-rename/java: string, bytes and flags defaults are
//     elided on form 1 when equal to their declared default (and fail when a
//     default is dropped), and a table rename with `was` decodes across the
//     rename with Unknown == 0.
//   - vt/renames/java: `was` on an enum variant, a union arm (with or without
//     payload) and a type field decodes across the rename with Unknown == 0.
func TestFixedClassOnTableWireJava(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{
		"tabledemo": true,
		"tblv1":     true,
		"tblv2":     true,
		"tblw1":     true,
		"tblw2":     true,
		"tblr1":     true,
		"tblr2":     true,
	}
	units := map[string]string{}
	fixtures := map[string][]javaVariableFixture{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" && selected[fields[1]] {
			units[fields[1]] = filepath.Join(repo, fields[2])
		}
		if len(fields) == 5 && fields[0] == "instance" && selected[fields[2]] {
			fixtures[fields[2]] = append(fixtures[fields[2]], javaVariableFixture{
				fields[1],
				fields[3],
				filepath.Join(repo, fields[4]),
				filepath.Join(repo, "testdata", "conformance", "tables", "json", fields[1]+".json"),
			})
		}
	}
	for _, name := range []string{"tabledemo", "tblv1", "tblv2", "tblw1", "tblw2", "tblr1", "tblr2"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if units[name] == "" || len(fixtures[name]) == 0 {
				t.Fatalf("manifest has no %s unit/instances", name)
			}
			javaVariableUnitFixtures(t, units[name], fixtures[name])
		})
	}

	t.Run("pure_fixed_closure", func(t *testing.T) {
		t.Parallel()
		testPureFixedClosureRoundTrip(t, repo)
	})

	t.Run("defaults_elided_and_negative_control", func(t *testing.T) {
		t.Parallel()
		testFixedClassDefaultsAndElision(t, repo)
	})

	t.Run("cross_generation_renames_and_reports", func(t *testing.T) {
		t.Parallel()
		testFixedClassCrossGenerationReports(t, repo)
	})
}

func unitFromPath(t *testing.T, p string) *ir.Unit {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		return unitFromDir(t, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f, perrs := parser.Parse(p, b)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", p, perrs[0])
	}
	name := filepath.Base(p)
	u, cerrs := check.Unit([]check.SourceFile{{
		Path:  p,
		Name:  name,
		Base:  strings.TrimSuffix(name, ".schema"),
		Bytes: b,
		AST:   f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", p, cerrs[0])
	}
	return u
}

func javaVariableUnitFixtures(t *testing.T, schemaPath string, fixtures []javaVariableFixture) {
	t.Helper()
	u := unitFromPath(t, schemaPath)
	runJavaVariableFixturesForUnit(t, u, fixtures)
}

func runJavaVariableFixturesForUnit(t *testing.T, u *ir.Unit, fixtures []javaVariableFixture) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TableVariable.java", "TableJson.java"} {
		if files[want] == nil {
			t.Fatalf("%s: %s is not emitted", u.Package, want)
		}
	}
	for _, row := range fixtures {
		if files[row.root+"Variable.java"] == nil {
			t.Fatalf("%s: %sVariable.java is not emitted", row.name, row.root)
		}
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
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	seen := map[string]bool{}
	for _, row := range fixtures {
		if seen[row.root] {
			continue
		}
		seen[row.root] = true
		name := "FixedClassDriver_" + row.root
		path := filepath.Join(dir, name+".java")
		source := strings.ReplaceAll(variableRoundTripDriver(pkg, row.root), "Driver", name)
		source = strings.Replace(source, "if (report.malformed || report.refused)", "if (report.malformed || report.refused || report.unknown != 0 || report.kindMismatch != 0 || report.clamped != 0 || report.widened != 0 || report.duplicate != 0)", 1)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, row := range fixtures {
		out, err := exec.Command(java, "-ea", "-cp", classes, "FixedClassDriver_"+row.root, row.wire, row.json).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "ok") {
			t.Errorf("%s: %v\n%s", row.name, err, out)
		}
	}
}

func testPureFixedClosureRoundTrip(t *testing.T, repo string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "PureFixed.schema")
	schema := `package purefixed

enum Team { Red, Blue, Green }

type Vec3
{
    x float32
    y float32
    z float32
}

fixed table BenchFixed
{
    tick    uint32
    time    float32
    team    Team
    tag     string(32)
    origin  Vec3
    samples [..8]int32
}
`
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromPath(t, schemaPath)
	model := tabletext.NewModel(u)
	inst := model.New(u.Tables["BenchFixed"])
	rep := new(tabletext.Report)
	if !model.Read(inst, []byte(`{"tick":42,"time":12.5,"team":"Blue","tag":"alpha","origin":{"x":1,"y":2,"z":3},"samples":[10,20,30]}`), rep) {
		t.Fatalf("model.Read: %+v", rep)
	}
	wire, err := tablewire.Encode(model, inst)
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(inst)
	if err != nil {
		t.Fatal(err)
	}
	wp := filepath.Join(dir, "bench.bin")
	jp := filepath.Join(dir, "bench.json")
	if err := os.WriteFile(wp, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jp, append(text, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files["BenchFixedVariable.java"] == nil || files["Vec3Variable.java"] == nil {
		t.Fatalf("Pure FixedTable unit did not emit BenchFixedVariable.java and Vec3Variable.java")
	}
	runJavaVariableFixturesForUnit(t, u, []javaVariableFixture{{"bench_fixed", "BenchFixed", wp, jp}})
}

func testFixedClassDefaultsAndElision(t *testing.T, repo string) {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Defaults.schema")
	schema := `package defprobe

enum Slot { Alpha, Beta }
flags Caps { Jump, Crouch, Fly }

type Badge
{
    label string(8) = "new"
    token bytes(4) = "xy"
}

fixed table Vessel
{
    name  string(32) = "untitled"
    tag   bytes(4) = "ab"
    caps  Caps = { Jump, Fly }
    badge Badge
    hull  int32 = 100 | min = 0, max = 1000
    per_slot [Slot]Badge
}
`
	if err := os.WriteFile(schemaPath, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := unitFromPath(t, schemaPath)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if files["VesselVariable.java"] == nil || files["BadgeVariable.java"] == nil {
		t.Fatalf("Defaults.schema: VesselVariable.java or BadgeVariable.java was not emitted")
	}
	model := tabletext.NewModel(u)
	defaultVessel := model.New(u.Tables["Vessel"])
	wantDefaultWire, err := tablewire.Encode(model, defaultVessel)
	if err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	wantPath := filepath.Join(dir, "vessel_default.bin")
	if err := os.WriteFile(wantPath, wantDefaultWire, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := `import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class DefaultDriver {
    public static void main(String[] args) throws Exception {
        byte[] want = Files.readAllBytes(Paths.get(args[0]));
        defprobe.VesselVariable.Value v = new defprobe.VesselVariable.Value();
        if (!"untitled".equals(v.name)) throw new AssertionError("name default: " + v.name);
        if (!Arrays.equals(v.tag, new byte[]{(byte) 'a', (byte) 'b'})) throw new AssertionError("tag default");
        if (v.caps != 5L) throw new AssertionError("caps default: " + v.caps);
        if (!"new".equals(v.badge.label)) throw new AssertionError("badge.label default: " + v.badge.label);
        if (!Arrays.equals(v.badge.token, new byte[]{(byte) 'x', (byte) 'y'})) throw new AssertionError("badge.token default");
        if (v.hull != 100) throw new AssertionError("hull default: " + v.hull);

        byte[] got = defprobe.VesselVariable.save(v);
        if (!Arrays.equals(want, got)) {
            throw new AssertionError("fresh Vessel save (" + got.length + " bytes) != Go default wire (" + want.length + " bytes)");
        }

        // Negative controls: dropping any string, bytes or flags default stops elision.
        defprobe.VesselVariable.Value droppedString = new defprobe.VesselVariable.Value();
        droppedString.name = "";
        if (Arrays.equals(want, defprobe.VesselVariable.save(droppedString))) {
            throw new AssertionError("dropped string default was still elided");
        }
        defprobe.VesselVariable.Value droppedBytes = new defprobe.VesselVariable.Value();
        droppedBytes.tag = new byte[0];
        if (Arrays.equals(want, defprobe.VesselVariable.save(droppedBytes))) {
            throw new AssertionError("dropped bytes default was still elided");
        }
        defprobe.VesselVariable.Value droppedFlags = new defprobe.VesselVariable.Value();
        droppedFlags.caps = 0L;
        if (Arrays.equals(want, defprobe.VesselVariable.save(droppedFlags))) {
            throw new AssertionError("dropped flags default was still elided");
        }
        defprobe.VesselVariable.Value droppedNestedString = new defprobe.VesselVariable.Value();
        droppedNestedString.badge.label = "";
        if (Arrays.equals(want, defprobe.VesselVariable.save(droppedNestedString))) {
            throw new AssertionError("dropped nested Badge.label default was still elided");
        }
        defprobe.VesselVariable.Value droppedKeyedBytes = new defprobe.VesselVariable.Value();
        droppedKeyedBytes.perSlot.get(0).token = new byte[0];
        if (Arrays.equals(want, defprobe.VesselVariable.save(droppedKeyedBytes))) {
            throw new AssertionError("dropped keyed Badge.token default was still elided");
        }
        System.out.println("ok");
    }
}
`
	driverPath := filepath.Join(dir, "DefaultDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, "DefaultDriver", wantPath).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("DefaultDriver: %v\n%s", err, out)
	}
}

func testFixedClassCrossGenerationReports(t *testing.T, repo string) {
	t.Helper()
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, s := range []string{"V1.schema", "V2.schema", "W1.schema", "W2.schema", "R1.schema", "R2.schema"} {
		u := unitFromPath(t, filepath.Join(repo, "test", "tables", s))
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		pkgDir := filepath.Join(dir, u.Package)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			p := filepath.Join(pkgDir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, p)
		}
	}
	driver := fmt.Sprintf(`import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class RenameDriver {
    static String rep(int u, int k, int w, int c, int d, boolean m, boolean r) {
        return u + "," + k + "," + w + "," + c + "," + d + "," + m + "," + (r ? "refused" : "read");
    }
    static void check(String name, String got, String want) {
        if (!want.equals(got)) {
            throw new AssertionError(name + ": got [" + got + "], want [" + want + "]");
        }
    }
    public static void main(String[] args) throws Exception {
        String wireDir = args[0];

        // w1_fleet_as_w2: Vessel renamed to Ship | was = "Vessel"
        {
            tblw2.TableVariable.Report r = new tblw2.TableVariable.Report();
            tblw2.FleetVariable.Value v = tblw2.FleetVariable.load(Files.readAllBytes(Paths.get(wireDir, "w1_fleet.bin")), r);
            if (v == null || r.refused) throw new AssertionError("w1_fleet_as_w2 refused");
            check("w1_fleet_as_w2", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "0,0,0,0,0,false,read");
            if (v.flagship == null || !"Aurora".equals(v.flagship.name) || v.escorts.size() != 2 || v.escorts.get(1) != v.flagship || v.flagship.hull != 250 || v.home.hull != 100) {
                throw new AssertionError("w1_fleet_as_w2 value mismatch");
            }
        }
        // w2_fleet_as_w1
        {
            tblw1.TableVariable.Report r = new tblw1.TableVariable.Report();
            tblw1.FleetVariable.Value v = tblw1.FleetVariable.load(Files.readAllBytes(Paths.get(wireDir, "w2_fleet.bin")), r);
            if (v == null || r.refused) throw new AssertionError("w2_fleet_as_w1 refused");
            check("w2_fleet_as_w1", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "0,0,0,0,0,false,read");
        }
        // w1_fleet_default_as_w2
        {
            tblw2.TableVariable.Report r = new tblw2.TableVariable.Report();
            tblw2.FleetVariable.Value v = tblw2.FleetVariable.load(Files.readAllBytes(Paths.get(wireDir, "w1_fleet_default.bin")), r);
            if (v == null || r.refused) throw new AssertionError("w1_fleet_default_as_w2 refused");
            check("w1_fleet_default_as_w2", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "0,0,0,0,0,false,read");
            if (v.flagship != null || !"fleet".equals(v.title) || !"untitled".equals(v.home.name) || !Arrays.equals(v.home.tag, new byte[]{(byte) 'a', (byte) 'b'}) || v.home.caps != 5L || !"new".equals(v.home.badge.label) || v.home.hull != 100) {
                throw new AssertionError("w1_fleet_default_as_w2 defaults did not land");
            }
        }
        // r1_cfg_as_r2: Silver -> Argent, ward -> shield, ping -> pong, Buff.multiplier -> mult
        {
            tblr2.TableVariable.Report r = new tblr2.TableVariable.Report();
            tblr2.CfgVariable.Value v = tblr2.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "r1_cfg.bin")), r);
            if (v == null || r.refused) throw new AssertionError("r1_cfg_as_r2 refused");
            check("r1_cfg_as_r2", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "0,0,0,0,0,false,read");
            if (v.grade != 2 || v.effect.tag != 2 || v.effect.shield.charge != 2.5f || v.buff.mult != 1.5f || v.tally.get(1) != 7) {
                throw new AssertionError("r1_cfg_as_r2 value mismatch");
            }
        }
        // r1_ping_as_r2: payload-free union arm ping -> pong
        {
            tblr2.TableVariable.Report r = new tblr2.TableVariable.Report();
            tblr2.CfgVariable.Value v = tblr2.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "r1_ping.bin")), r);
            if (v == null || r.refused) throw new AssertionError("r1_ping_as_r2 refused");
            check("r1_ping_as_r2", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "0,0,0,0,0,false,read");
            if (v.effect.tag != 3) {
                throw new AssertionError("r1_ping_as_r2 pong tag mismatch: " + v.effect.tag);
            }
        }
        // r2_cfg_as_r1
        {
            tblr1.TableVariable.Report r = new tblr1.TableVariable.Report();
            tblr1.CfgVariable.Value v = tblr1.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "r2_cfg.bin")), r);
            if (v == null || r.refused) throw new AssertionError("r2_cfg_as_r1 refused");
            check("r2_cfg_as_r1", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "0,0,0,0,0,false,read");
        }
        // v1_cfg_as_v2 and v1_seams_as_v2
        {
            tblv2.TableVariable.Report r = new tblv2.TableVariable.Report();
            tblv2.CfgVariable.Value v = tblv2.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "v1_cfg.bin")), r);
            if (v == null || r.refused) throw new AssertionError("v1_cfg_as_v2 refused");
            check("v1_cfg_as_v2", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "1,1,0,1,0,false,read");
        }
        {
            tblv2.TableVariable.Report r = new tblv2.TableVariable.Report();
            tblv2.CfgVariable.Value v = tblv2.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "v1_seams.bin")), r);
            if (v == null || r.refused) throw new AssertionError("v1_seams_as_v2 refused");
            check("v1_seams_as_v2", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "2,2,0,0,0,false,read");
        }
        // v2_cfg_as_v1 and v2_seams_as_v1
        {
            tblv1.TableVariable.Report r = new tblv1.TableVariable.Report();
            tblv1.CfgVariable.Value v = tblv1.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "v2_cfg.bin")), r);
            if (v == null || r.refused) throw new AssertionError("v2_cfg_as_v1 refused");
            check("v2_cfg_as_v1", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "5,1,0,1,0,false,read");
        }
        {
            tblv1.TableVariable.Report r = new tblv1.TableVariable.Report();
            tblv1.CfgVariable.Value v = tblv1.CfgVariable.load(Files.readAllBytes(Paths.get(wireDir, "v2_seams.bin")), r);
            if (v == null || r.refused) throw new AssertionError("v2_seams_as_v1 refused");
            check("v2_seams_as_v1", rep(r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed, r.refused), "2,2,0,0,0,false,read");
        }
        System.out.println("ok");
    }
}
`)
	driverPath := filepath.Join(dir, "RenameDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	out, err := exec.Command(java, "-ea", "-cp", classes, "RenameDriver", filepath.Join(repo, "testdata", "wire", "tables")).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("RenameDriver: %v\n%s", err, out)
	}
}
