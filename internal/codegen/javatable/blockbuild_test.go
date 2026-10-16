package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBlockBuildAndCookWriteJava pins the producer entries of the two
// accelerators (docs/SPEC-TABLES.md §19.1 and §7.6): the block builder
// (<T>Block.Counts, begin, blockBytes, Refusal) byte-equal to the two images the
// C++ producer test/tables/block_main.cpp committed, and the cook writer
// (<T>Cook.cookMeasure, cookMeasureReason, cookFrom; the Java spelling of Go's
// <T>CookMeasure, <T>CookMeasureReason and <T>CookFrom) byte-equal to the two
// files `schema cook` wrote, in TableByteOrder.LITTLE and BIG.
func TestBlockBuildAndCookWriteJava(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFromDir(t, "../../../tables/block"))
	if err != nil {
		t.Fatal(err)
	}
	block := string(files["PaddedFrameBlock.java"])
	for _, name := range []string{"public static PaddedFrameBlock begin(byte[] storage, int base, Counts counts, Refusal refusal)",
		"public long blockBytes()", "public static final class Counts", "public static final class Refusal"} {
		if !strings.Contains(block, name) {
			t.Errorf("PaddedFrameBlock lacks %q: docs/SPEC-TABLES.md §19.1 build side", name)
		}
	}
	// A table with no Java value class has no writer, and its file says so
	// rather than offering an untyped entry (§7.6).
	if cook := string(files["PaddedFrameCook.java"]); strings.Contains(cook, "cookFrom(") || !strings.Contains(cook, "NO WRITER for PaddedFrame") {
		t.Errorf("PaddedFrameCook: want no writer and the NO WRITER line (blockdemo generates no PaddedFrameVariable)")
	}
	graph, err := Generate(unitFromDir(t, "../../../tables/pointers"))
	if err != nil {
		t.Fatal(err)
	}
	scene := string(graph["SceneCook.java"])
	for _, name := range []string{
		"public static long cookMeasure(SceneVariable.Value value)",
		"public static TableCookWrite.MeasureReason cookMeasureReason(SceneVariable.Value value)",
		"public static boolean cookFrom(SceneVariable.Value value, byte[] buffer, int order)",
	} {
		if !strings.Contains(scene, name) {
			t.Errorf("SceneCook lacks %q: docs/SPEC-TABLES.md §7.6 write side", name)
		}
	}
	order := string(graph["TableByteOrder.java"])
	for _, name := range []string{"public static final int LITTLE = 1;", "public static final int BIG = 2;"} {
		if !strings.Contains(order, name) {
			t.Errorf("TableByteOrder lacks %q: Go's TableByteOrderLittle/Big values (§7.6)", name)
		}
	}
	writer := string(graph["TableCookWrite.java"])
	for _, name := range []string{`REASON_OK = "ok"`, `REASON_INVALID_VALUE = "invalid_value"`} {
		if !strings.Contains(writer, name) {
			t.Errorf("TableCookWrite lacks %q: Go's TableRefuseReason names (gotable/refusal.go)", name)
		}
	}
	t.Run("compile", func(t *testing.T) {
		t.Parallel()
		javac, _ := javaTools(t)
		dir := t.TempDir()
		var sources []string
		for name, body := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
		args := append([]string{"--release", "17", "-d", dir}, sources...)
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
	})
	t.Run("block_images", func(t *testing.T) {
		t.Parallel()
		javac, javaBin := javaTools(t)
		dir := t.TempDir()
		srcDir := filepath.Join(dir, "blockdemo")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var sources []string
		for name, body := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			path := filepath.Join(srcDir, name)
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
		driver := filepath.Join(srcDir, "BlockGoldenDriver.java")
		if err := os.WriteFile(driver, []byte(blockGoldenDriver), 0o644); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"--release", "17", "-d", dir}, sources...)
		args = append(args, driver)
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		root := filepath.Join("..", "..", "..", "testdata", "wire", "tables")
		cmd := exec.Command(javaBin, "-ea", "-cp", dir, "blockdemo.BlockGoldenDriver",
			filepath.Join(root, "block_render.bin"), filepath.Join(root, "block_padded.bin"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("java block: %v\n%s", err, out)
		}
	})
	for _, tc := range []struct{ name, dir, pkg, root, wire, little, big string }{
		{"graph_tree", "../../../tables/pointers", "graphdemo", "Scene", "graph_tree.bin", "graph_tree.cook", "graph_tree-be.cook"},
		{"graph_shared", "../../../tables/pointers", "graphdemo", "Scene", "graph_shared.bin", "graph_shared.cook", "graph_shared-be.cook"},
		{"blob_small", "../../../tables/blobs", "blobdemo", "Catalog", "blob_small.bin", "blob_small.cook", "blob_small-be.cook"},
		{"blob_shared", "../../../tables/blobs", "blobdemo", "Catalog", "blob_shared.bin", "blob_shared.cook", "blob_shared-be.cook"},
	} {
		t.Run("cook_"+tc.name, func(t *testing.T) {
			t.Parallel()
			cookGolden(t, tc.dir, tc.pkg, tc.root, tc.wire, tc.little, tc.big)
		})
	}
}

// cookGolden loads the committed table wire and writes the two schema cook
// reference images (§7.6), including refusal paths that must not touch output.
func cookGolden(t *testing.T, schemaDir, pkg, root, wire, little, big string) {
	t.Helper()
	javac, javaBin := javaTools(t)
	files, err := Generate(unitFromDir(t, schemaDir))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	srcDir := filepath.Join(dir, pkg)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, body := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		path := filepath.Join(srcDir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	driver := strings.NewReplacer("PKG", pkg, "ROOT", root).Replace(cookGoldenDriver)
	driverPath := filepath.Join(dir, "CookGoldenDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	args = append(args, driverPath)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	rootPath := filepath.Join("..", "..", "..", "testdata")
	cmd := exec.Command(javaBin, "-ea", "-cp", dir, "CookGoldenDriver",
		filepath.Join(rootPath, "wire", "tables", wire),
		filepath.Join(rootPath, "conformance", "tables", "cook-write", little),
		filepath.Join(rootPath, "conformance", "tables", "cook-write", big))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("java cook %s: %v\n%s", wire, err, out)
	}
}

const cookGoldenDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class CookGoldenDriver {
    private static void untouched(byte[] b, String what) {
        for (byte x : b) if (x != (byte) 0x5a) throw new AssertionError(what + " wrote bytes");
    }
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Path.of(args[0]));
        PKG.TableVariable.Report report = new PKG.TableVariable.Report();
        PKG.ROOTVariable.Value value = PKG.ROOTVariable.load(wire, report);
        if (value == null || report.refused || report.malformed) throw new AssertionError("wire load");
        long n = PKG.ROOTCook.cookMeasure(value);
        PKG.TableCookWrite.MeasureReason reason = PKG.ROOTCook.cookMeasureReason(value);
        if (n < 0 || reason.bytes() != n || !reason.reason().equals("ok")) throw new AssertionError("measure " + reason);
        byte[] out = new byte[(int) n];
        Arrays.fill(out, (byte) 0x5a);
        // §7.6's refusals: a short buffer, an order neither little nor big, no buffer.
        byte[] shortBuf = new byte[(int) n - 1];
        Arrays.fill(shortBuf, (byte) 0x5a);
        if (PKG.ROOTCook.cookFrom(value, shortBuf, PKG.TableByteOrder.LITTLE)) throw new AssertionError("short accepted");
        untouched(shortBuf, "short refusal");
        if (PKG.ROOTCook.cookFrom(value, out, 0)) throw new AssertionError("order 0 accepted");
        if (PKG.ROOTCook.cookFrom(value, out, 3)) throw new AssertionError("order 3 accepted");
        if (PKG.ROOTCook.cookFrom(value, null, PKG.TableByteOrder.LITTLE)) throw new AssertionError("null buffer accepted");
        untouched(out, "order refusal");
        for (int order : new int[] {PKG.TableByteOrder.LITTLE, PKG.TableByteOrder.BIG}) {
            if (!PKG.ROOTCook.cookFrom(value, out, order)) throw new AssertionError("cook order " + order);
            byte[] want = Files.readAllBytes(Path.of(args[order]));
            if (!Arrays.equals(out, want)) {
                int at = Arrays.mismatch(out, want);
                throw new AssertionError("cook bytes order " + order + " first mismatch " + at +
                        " got " + (at < out.length ? (out[at] & 255) : -1) +
                        " want " + (at < want.length ? (want[at] & 255) : -1));
            }
        }
        // invalid_value (Go's TableRefuseInvalidValue): the root's string(N) name
        // at N bytes cooks, at N + 1 it is refused by measure and by write alike.
        int bound = -1;
        for (PKG.TableCookFieldInfo f : PKG.ROOTCook.type().fields) if (f.name.equals("name")) bound = f.arrayBound;
        if (bound < 1) throw new AssertionError("no name bound");
        value.name = "n".repeat(bound);
        if (PKG.ROOTCook.cookMeasure(value) < 0) throw new AssertionError("name at its bound refused");
        value.name = "n".repeat(bound + 1);
        PKG.TableCookWrite.MeasureReason bad = PKG.ROOTCook.cookMeasureReason(value);
        if (PKG.ROOTCook.cookMeasure(value) != -1 || bad.bytes() != -1 || !bad.reason().equals("invalid_value"))
            throw new AssertionError("over-long name: " + bad);
        Arrays.fill(out, (byte) 0x5a);
        if (PKG.ROOTCook.cookFrom(value, out, PKG.TableByteOrder.LITTLE)) throw new AssertionError("invalid value written");
        untouched(out, "invalid_value refusal");
        System.out.println("cook-write ok " + n);
    }
}
`

const blockGoldenDriver = `package blockdemo;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class BlockGoldenDriver {
    private static TableBlockFieldInfo field(TableBlockInfo info, String name) {
        for (TableBlockFieldInfo f : info.fields) if (f.name.equals(name)) return f;
        throw new AssertionError(info.name + "." + name);
    }
    private static void put(byte[] b, int at, TableBlockInfo info, String name, long value, int width) {
        TableBytes.put(b, at + field(info, name).offset, value, width, false);
    }
    private static void dbl(byte[] b, int at, TableBlockInfo info, String name, double value) {
        put(b, at, info, name, Double.doubleToRawLongBits(value), 8);
    }
    private static void flt(byte[] b, int at, TableBlockInfo info, String name, float value) {
        put(b, at, info, name, Float.floatToRawIntBits(value), 4);
    }
    private static void vec(byte[] b, int at, int salt, int i) {
        TableBlockInfo info = RenderVector3Row.blockInfo();
        dbl(b, at, info, "x", 1.5 * i + salt);
        dbl(b, at, info, "y", 2.25 * i - salt);
        dbl(b, at, info, "z", 0.5 * i + 2 * salt);
    }
    private static void quat(byte[] b, int at, int salt, int i) {
        TableBlockInfo info = RenderQuaternionRow.blockInfo();
        dbl(b, at, info, "x", 0.25 * i + salt);
        dbl(b, at, info, "y", 0.125 * i);
        dbl(b, at, info, "z", 0.0625 * i - salt);
        dbl(b, at, info, "w", 1.0 - 0.5 * (i % 3));
    }
    private static void common(byte[] b, int at, TableBlockInfo info, int salt, int i,
                               boolean position, boolean rotation) {
        if (position) vec(b, at + field(info, "position").offset, salt, i);
        if (rotation) quat(b, at + field(info, "rotation").offset, salt, i);
    }
    private static void assertBytes(byte[] b, int used, String golden) throws Exception {
        byte[] want = Files.readAllBytes(Path.of(golden));
        byte[] got = Arrays.copyOf(b, used);
        if (!Arrays.equals(got, want)) {
            int at = Arrays.mismatch(got, want);
            throw new AssertionError(golden + " first mismatch " + at + " got " +
                    (at < got.length ? got[at] & 255 : -1) + " want " +
                    (at < want.length ? want[at] & 255 : -1));
        }
    }
    private static void padded(String golden) throws Exception {
        byte[] b = new byte[(int) PaddedFrameBlock.blockMaxBytes];
        PaddedFrameBlock.Counts c = new PaddedFrameBlock.Counts(); c.rows = 4;
        PaddedFrameBlock.Refusal refusal = new PaddedFrameBlock.Refusal();
        PaddedFrameBlock block = PaddedFrameBlock.begin(b, c, refusal);
        if (block == null || PaddedFrameBlock.open(b, 0, block.blockBytes()) == null) {
            throw new AssertionError("padded begin/open " + refusal.array);
        }
        TableBlockInfo frame = PaddedFrameBlock.type();
        put(b, 0, frame, "marker", 7, 1);
        put(b, 0, frame, "stamp", 0x0123456789abcdefL, 8);
        byte[] blob = "twelve bytes".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        TableBlockFieldInfo bf = field(frame, "blob");
        System.arraycopy(blob, 0, b, bf.offset, blob.length);
        TableBytes.put(b, bf.countOffset, blob.length, 4, false);
        TableBlockInfo row = PaddedRowRow.blockInfo();
        for (int i = 0; i < c.rows; i++) {
            int at = block.rowsAt(i);
            put(b, at, row, "tag", 10 + i, 1);
            dbl(b, at, row, "value", 0.5 * i + 100.0);
            put(b, at, row, "flag", i % 2 == 0 ? 1 : 0, 1);
            put(b, at, row, "id", i * 1000 + 3, 4);
            byte[] label = ("row-" + i).getBytes(java.nio.charset.StandardCharsets.UTF_8);
            TableBlockFieldInfo lf = field(row, "label");
            System.arraycopy(label, 0, b, at + lf.offset, label.length);
            TableBytes.put(b, at + lf.countOffset, label.length, 4, false);
            for (int s = 0; s < 4; s++) TableBytes.put(b, at + field(row, "slots").offset + s * 2,
                    i * 4 + s, 2, false);
            for (int team = 1; team <= 4; team++) b[at + field(row, "teams").offset + team - 1] = (byte) (i + team);
            TableBlockFieldInfo cf = field(row, "counter");
            TableBytes.put(b, at + cf.offset, i * 9, 4, false);
            b[at + cf.presentOffset] = (byte) (i % 2);
        }
        assertBytes(b, (int) block.blockBytes(), golden);
        Arrays.fill(b, (byte) 0x5a);
        c.rows = 65;
        if (PaddedFrameBlock.begin(b, c, refusal) != null || !"rows".equals(refusal.array)
                || refusal.count != 65 || refusal.maximum != 64) throw new AssertionError("count refusal");
        for (byte x : b) if (x != (byte) 0x5a) throw new AssertionError("begin refusal wrote bytes");
    }
    private static void render(String golden) throws Exception {
        byte[] b = new byte[(int) RenderFrameBlock.blockMaxBytes];
        RenderFrameBlock.Counts c = new RenderFrameBlock.Counts();
        c.cameras = 1; c.ships = 5; c.turrets = 7; c.missiles = 3;
        c.dynamicProps = 2; c.staticProps = 4; c.cosmeticProps = 3;
        c.lasers = 6; c.explosions = 2;
        RenderFrameBlock block = RenderFrameBlock.begin(b, c, new RenderFrameBlock.Refusal());
        if (block == null || RenderFrameBlock.open(b, 0, block.blockBytes()) == null) throw new AssertionError("render begin/open");
        put(b, 0, RenderFrameBlock.type(), "version", 1, 8);
        for (int i = 0; i < c.cameras; i++) {
            int at = block.camerasAt(i); TableBlockInfo r = RenderCameraRow.blockInfo();
            common(b, at, r, 1, i, true, true);
            put(b, at, r, "camera_id", i * 7 + 1, 4); put(b, at, r, "camera_type", i % 4, 4);
            put(b, at, r, "target_object_id", i * 13 + 2, 4); flt(b, at, r, "fov", 0.5f * i + 60.0f);
        }
        for (int i = 0; i < c.ships; i++) {
            int at = block.shipsAt(i); TableBlockInfo r = RenderShipRow.blockInfo();
            common(b, at, r, 2, i, true, true);
            put(b, at, r, "flags", i % 16, 8); put(b, at, r, "object_id", i * 3 + 11, 4);
            put(b, at, r, "target_object_id", i * 5 + 7, 4); flt(b, at, r, "thrust", 0.25f * i);
            put(b, at, r, "object_sequence", i % 251, 1); put(b, at, r, "ship_type", i % 4, 1);
            put(b, at, r, "team", i % 5, 1); put(b, at, r, "has_target_lock", i % 2 == 0 ? 1 : 0, 1);
            put(b, at, r, "predicted_explode", i % 3 == 0 ? 1 : 0, 1);
        }
        for (int i = 0; i < c.turrets; i++) {
            int at = block.turretsAt(i); TableBlockInfo r = RenderTurretRow.blockInfo();
            common(b, at, r, 3, i, false, true);
            put(b, at, r, "flags", i * 17, 8); put(b, at, r, "object_id", i * 2 + 1, 4);
            put(b, at, r, "parent_object_id", i / 3, 4); put(b, at, r, "turret_index", i % 8, 4);
            put(b, at, r, "target_object_id", i * 11, 4); put(b, at, r, "object_sequence", i % 253, 1);
            put(b, at, r, "team", i % 5, 1); put(b, at, r, "has_target_lock", i % 5 == 0 ? 1 : 0, 1);
        }
        for (int i = 0; i < c.missiles; i++) {
            int at = block.missilesAt(i); TableBlockInfo r = RenderMissileRow.blockInfo();
            common(b, at, r, 4, i, true, true);
            put(b, at, r, "flags", i * 19, 8); put(b, at, r, "object_id", i * 23, 4);
            put(b, at, r, "object_sequence", i % 249, 1); put(b, at, r, "missile_type", i % 3, 1);
            put(b, at, r, "team", i % 5, 1);
        }
        for (int i = 0; i < c.dynamicProps; i++) {
            int at = block.dynamicPropsAt(i); TableBlockInfo r = RenderDynamicPropRow.blockInfo();
            common(b, at, r, 5, i, true, true);
            put(b, at, r, "flags", i * 29, 8); put(b, at, r, "object_id", i * 31, 4);
            put(b, at, r, "object_sequence", i % 247, 1); put(b, at, r, "prop_type", i % 4, 1);
            put(b, at, r, "team", i % 5, 1);
        }
        for (int i = 0; i < c.staticProps; i++) {
            int at = block.staticPropsAt(i); TableBlockInfo r = RenderStaticPropRow.blockInfo();
            common(b, at, r, 6, i, true, true);
            dbl(b, at, r, "scale", 0.5 + 0.25 * (i % 7)); put(b, at, r, "flags", i * 37, 8);
            put(b, at, r, "static_prop_id", i * 41, 4); put(b, at, r, "prop_type", i % 4, 1);
            put(b, at, r, "team", i % 5, 1);
        }
        for (int i = 0; i < c.cosmeticProps; i++) {
            int at = block.cosmeticPropsAt(i); TableBlockInfo r = RenderCosmeticPropRow.blockInfo();
            common(b, at, r, 7, i, true, true);
            dbl(b, at, r, "scale", 0.25 + 0.125 * (i % 5)); put(b, at, r, "flags", i * 43, 8);
            put(b, at, r, "cosmetic_prop_id", i * 47, 4); put(b, at, r, "prop_sequence", i % 241, 1);
            put(b, at, r, "prop_type", i % 4, 1); put(b, at, r, "team", i % 5, 1);
        }
        for (int i = 0; i < c.lasers; i++) {
            int at = block.lasersAt(i); TableBlockInfo r = RenderLaserRow.blockInfo();
            vec(b, at + field(r, "start").offset, 8, i); vec(b, at + field(r, "finish").offset, 9, i);
            dbl(b, at, r, "t", 0.125 * (i % 8)); put(b, at, r, "laser_id", i * 53, 4);
            put(b, at, r, "laser_type", i % 3, 1); put(b, at, r, "team", i % 5, 1);
        }
        for (int i = 0; i < c.explosions; i++) {
            int at = block.explosionsAt(i); TableBlockInfo r = RenderExplosionRow.blockInfo();
            common(b, at, r, 10, i, true, true);
            dbl(b, at, r, "t", 0.0625 * (i % 16)); put(b, at, r, "explosion_id", i * 59, 4);
            put(b, at, r, "parent_object_id", i * 61, 4); put(b, at, r, "explosion_type", i % 3, 1);
            put(b, at, r, "team", i % 5, 1);
        }
        assertBytes(b, (int) block.blockBytes(), golden);
    }
    public static void main(String[] args) throws Exception { render(args[0]); padded(args[1]); }
}
`
