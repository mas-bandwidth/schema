package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestBitsFieldAndArrayWidthClampJava is the regression test for the wire-fuzz
// finding on seed archive (tabledemo.ArchiveConfig), pass random #83429,
// SEED=24845619678 (byte 44 mutated 0x2d -> 0xe8 in WeaponConfig.channel,
// declared as bits(6) without an explicit range annotation). On the variable
// wire, bits(N) has an implicit raw range of 0 .. (1<<N)-1 (SPEC-TABLES §3.3,
// §4; internal/tablewire/decode.go): a wire value above (1<<N)-1 clamps to
// (1<<N)-1 and increments report.clamped.
func TestBitsFieldAndArrayWidthClampJava(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	build := t.TempDir()
	var sources []string

	u := unitFromDir(t, filepath.Join(repo, "tables", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range pkt {
		if _, ok := files[name]; !ok && strings.HasSuffix(name, ".java") {
			files[name] = data
		}
	}
	exDir := filepath.Join(build, "examples")
	if err := os.MkdirAll(exDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		path := filepath.Join(exDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	minSchemaDir := filepath.Join(build, "minischema")
	if err := os.MkdirAll(minSchemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	minSchema := `package bitsclampdemo

table MiniLane {
    ch bits(6)
}

table MiniBits {
    small      bits(6)
    wide       bits(37)
    lanes      [..4]uint8
    wide_lanes [..4]uint64
    items      [..4]MiniLane
}
`
	if err := os.WriteFile(filepath.Join(minSchemaDir, "MiniBits.schema"), []byte(minSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	mu := unitFromDir(t, minSchemaDir)
	// Exercise both writeIntFieldRead (scalar bits(6) and bits(37), plus
	// table-wrapped items [..4]MiniLane) and writeArrayElemRead on direct
	// ir.TBits array elements ([..4]bits(6) and [..4]bits(37)).
	for _, f := range mu.Tables["MiniBits"].Fields {
		switch f.Name {
		case "lanes":
			f.Type.Kind = ir.TBits
			f.Type.Width = 6
		case "wide_lanes":
			f.Type.Kind = ir.TBits
			f.Type.Width = 37
		}
	}
	mfiles, err := Generate(mu)
	if err != nil {
		t.Fatal(err)
	}
	mpkt, err := javacodegen.Generate(mu)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range mpkt {
		if _, ok := mfiles[name]; !ok && strings.HasSuffix(name, ".java") {
			mfiles[name] = data
		}
	}
	mDir := filepath.Join(build, "minibits")
	if err := os.MkdirAll(mDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range mfiles {
		path := filepath.Join(mDir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	driver := `import java.nio.file.Files;
import java.nio.file.Paths;

public final class BitsClampDriver {
    private BitsClampDriver() {}

    public static void main(String[] args) throws Exception {
        byte[] mutant = Files.readAllBytes(Paths.get(args[0]));
        tabledemo.TableVariable.Report rep = new tabledemo.TableVariable.Report();
        tabledemo.ArchiveConfigVariable.Value v = tabledemo.ArchiveConfigVariable.load(mutant, rep);
        byte[] saved = v == null ? null : tabledemo.ArchiveConfigVariable.save(v);
        tabledemo.TableVariable.Report rep2 = new tabledemo.TableVariable.Report();
        tabledemo.ArchiveConfigVariable.Value v2 = saved == null ? null : tabledemo.ArchiveConfigVariable.load(saved, rep2);
        System.out.println("archive loaded=" + (v != null)
                + " channel=" + (v == null ? -1 : v.root.weapons.get(0).channel)
                + " clamped=" + rep.clamped
                + " malformed=" + rep.malformed
                + " refused=" + rep.refused
                + " saved=" + (saved == null ? -1 : saved.length)
                + " reloaded=" + (v2 == null ? -1 : v2.root.weapons.get(0).channel)
                + " reclamped=" + rep2.clamped);

        tabledemo.TableRetain retain = new tabledemo.TableRetain(1 << 20, 1024);
        tabledemo.TableVariable.Report rrep = new tabledemo.TableVariable.Report();
        tabledemo.ArchiveConfigVariable.Value rv = tabledemo.ArchiveConfigRetain.loadRetain(mutant, retain, rrep);
        byte[] rsaved = rv == null ? null : tabledemo.ArchiveConfigRetain.saveRetain(rv, retain, rrep);
        System.out.println("archive-retain loaded=" + (rv != null)
                + " channel=" + (rv == null ? -1 : rv.root.weapons.get(0).channel)
                + " clamped=" + rrep.clamped
                + " saved=" + (rsaved == null ? -1 : rsaved.length));

        // Minimized reproducer covering bits(6) scalar, bits(37) >32-bit scalar,
        // [..4]bits(6) array, [..4]bits(37) array, and [..4]MiniLane table array:
        bitsclampdemo.MiniBitsVariable.Value m = new bitsclampdemo.MiniBitsVariable.Value();
        m.small = 45;
        m.wide = 0x15_0000_0000L;
        m.lanes.add(12);
        m.lanes.add(46);
        m.lanes.add(63);
        m.wideLanes.add(7L);
        m.wideLanes.add(0x16_0000_0000L);
        bitsclampdemo.MiniLaneVariable.Value item0 = new bitsclampdemo.MiniLaneVariable.Value();
        item0.ch = 47;
        bitsclampdemo.MiniLaneVariable.Value item1 = new bitsclampdemo.MiniLaneVariable.Value();
        item1.ch = 63;
        m.items.add(item0);
        m.items.add(item1);
        byte[] mwire = bitsclampdemo.MiniBitsVariable.save(m);
        for (int i = 0; i < mwire.length; i++) {
            if (mwire[i] == 45) {
                mwire[i] = (byte) 0xe8;
                break;
            }
        }
        for (int i = 0; i < mwire.length; i++) {
            if (mwire[i] == 46) {
                mwire[i] = (byte) 0xc8;
                break;
            }
        }
        for (int i = 0; i < mwire.length; i++) {
            if (mwire[i] == 47) {
                mwire[i] = (byte) 0xf0;
                break;
            }
        }
        for (int i = 0; i < mwire.length - 4; i++) {
            if (mwire[i] == 0x00 && mwire[i + 1] == 0x00 && mwire[i + 2] == 0x00 && mwire[i + 3] == 0x00 && mwire[i + 4] == 0x15) {
                mwire[i + 4] = (byte) 0xff;
                break;
            }
        }
        for (int i = 0; i < mwire.length - 4; i++) {
            if (mwire[i] == 0x00 && mwire[i + 1] == 0x00 && mwire[i + 2] == 0x00 && mwire[i + 3] == 0x00 && mwire[i + 4] == 0x16) {
                mwire[i + 4] = (byte) 0xfe;
                break;
            }
        }
        bitsclampdemo.TableVariable.Report mrep = new bitsclampdemo.TableVariable.Report();
        bitsclampdemo.MiniBitsVariable.Value ml = bitsclampdemo.MiniBitsVariable.load(mwire, mrep);
        bitsclampdemo.TableRetain mret = new bitsclampdemo.TableRetain(1 << 20, 1024);
        bitsclampdemo.TableVariable.Report mrrep = new bitsclampdemo.TableVariable.Report();
        bitsclampdemo.MiniBitsVariable.Value mrl = bitsclampdemo.MiniBitsRetain.loadRetain(mwire, mret, mrrep);
        System.out.println("mini small=" + ml.small
                + " wide=" + Long.toHexString(ml.wide)
                + " lanes=" + ml.lanes
                + " wideLanes=" + Long.toHexString(ml.wideLanes.get(0)) + "," + Long.toHexString(ml.wideLanes.get(1))
                + " items=" + ml.items.get(0).ch + "," + ml.items.get(1).ch
                + " clamped=" + mrep.clamped
                + " retainClamped=" + mrrep.clamped
                + " retainLanes=" + mrl.lanes);
    }
}
`
	driverPath := filepath.Join(build, "BitsClampDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driverPath)
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	mutantPath, err := filepath.Abs(filepath.Join(repo, "testdata", "wire", "tables", "fuzz-vectors", "bits_width_clamp_archive_r83429.bin"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "BitsClampDriver", mutantPath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	want := strings.Join([]string{
		"archive loaded=true channel=63 clamped=1 malformed=false refused=false saved=598 reloaded=63 reclamped=0",
		"archive-retain loaded=true channel=63 clamped=1 saved=598",
		"mini small=63 wide=1fffffffff lanes=[12, 63, 63] wideLanes=7,1fffffffff items=63,63 clamped=5 retainClamped=5 retainLanes=[12, 63, 63]",
	}, "\n")
	if got != want {
		t.Fatalf("bits(N) width clamp mismatch:\n%s\nwant:\n%s", got, want)
	}
}
