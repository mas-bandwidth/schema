package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	javapkt "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

// TestBlockBuildAndCookWriteJava pins the producer entries of the two
// accelerators (docs/SPEC-TABLES.md §19.1 and §7.6). The runtime subtests
// below exercise the committed C++ block images and schema cook bytes.
func TestBlockBuildAndCookWriteJava(t *testing.T) {
	t.Parallel()
	files, err := Generate(unitFromDir(t, "../../../tables/block"))
	if err != nil {
		t.Fatal(err)
	}
	block := string(files["PaddedFrameBlock.java"])
	for _, name := range []string{"begin(", "blockBytes(", "class Counts"} {
		if !strings.Contains(block, name) {
			t.Errorf("PaddedFrameBlock lacks %s: docs/SPEC-TABLES.md §19.1 build side", name)
		}
	}
	cook := string(files["PaddedFrameCook.java"])
	for _, name := range []string{"cookMeasure(", "cookMeasureReason(", "cookFrom("} {
		if !strings.Contains(cook, name) {
			t.Errorf("PaddedFrameCook lacks %s: docs/SPEC-TABLES.md §7.6 write side", name)
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
	t.Run("cook_rows_partition", func(t *testing.T) {
		t.Parallel()
		cookWritePartition(t, readCookManifest(t))
	})
	t.Run("cook_absent_is_structural", func(t *testing.T) {
		t.Parallel()
		cookWriteAbsenceIsStructural(t)
	})
	for _, u := range cookWriteCarried {
		t.Run("cook_"+u.pkg, func(t *testing.T) {
			t.Parallel()
			cookGoldenUnit(t, readCookManifest(t), u)
		})
	}
	t.Run("cook_refusal_invalid_value", func(t *testing.T) {
		t.Parallel()
		cookRefusal(t, readCookManifest(t))
	})
}

// THE COOK-WRITE ROWS, partitioned (docs/SPEC-TABLES.md §7.6). Every
// `cook-write` row of testdata/conformance/tables/MANIFEST.txt is in exactly
// one of the three lists below, and cook_rows_partition holds the lists to the
// manifest, so a row added there without a Java answer is a red test here.
//
// cookWriteCarried: the Java cook writer carries the root, and each row is
// written in both byte orders and compared byte for byte with `schema cook`.
type cookWriteRoot struct {
	root string
	rows []string
}

type cookWriteUnit struct {
	schema string // a unit directory, or one .schema file
	unit   string // the manifest's unit key
	pkg    string // the unit's Java package
	roots  []cookWriteRoot
}

var cookWriteCarried = []cookWriteUnit{
	{"../../../tables/blobs", "blobdemo", "blobdemo", []cookWriteRoot{
		{"Catalog", []string{"blob_small", "blob_empty", "blob_large", "blob_shared", "blob_str8", "blob_str16"}},
	}},
	{"../../../tables/pointers", "graphdemo", "graphdemo", []cookWriteRoot{
		{"Scene", []string{"graph_tree", "graph_shared", "graph_empty", "graph_deep"}},
	}},
	{"../../../test/tables/P1.schema", "tblp1", "tblp1", []cookWriteRoot{
		{"Chain", []string{"chain_value", "chain_value_empty"}},
	}},
	{"../../../test/tables/P3.schema", "tblp3", "tblp3", []cookWriteRoot{
		{"Chain", []string{"chain_optional", "chain_optional_empty"}},
	}},
	{"../../../tables/examples", "tabledemo", "tabledemo", []cookWriteRoot{
		{"KeyedConfig", []string{"keyed_config", "keyed_default"}},
		{"WideBlob", []string{"wide_blob"}},
		{"RootConfig", []string{"root_full", "root_default"}},
		{"ProfileConfig", []string{"profile_elide"}},
		{"LoadoutConfig", []string{"loadout_full"}},
		{"ArchiveConfig", []string{"archive"}},
	}},
	{"../../../test/tables/V1.schema", "tblv1", "tblv1", []cookWriteRoot{
		{"Cfg", []string{"v1_cfg", "v1_seams"}},
	}},
	{"../../../test/tables/V2.schema", "tblv2", "tblv2", []cookWriteRoot{
		{"Cfg", []string{"v2_cfg", "v2_seams"}},
	}},
	{"../../../tables/messages", "messagedemo", "messagedemo", []cookWriteRoot{
		{"ToolMessage", []string{"message_open", "message_transaction", "message_ping", "message_save",
			"message_empty", "message_batch", "message_arms", "message_arm_edits", "message_ack", "message_trace"}},
	}},
	{"../../../tables/stream", "streamdemo", "streamdemo", []cookWriteRoot{
		{"Feed", []string{"stream_chain", "stream_header", "stream_parts", "stream_arm_first", "stream_arm_pointer"}},
	}},
	{"../../../test/tables/M1.schema", "tblm1", "tblm1", []cookWriteRoot{
		{"Msg", []string{"m1_open"}},
	}},
	{"../../../test/tables/M2.schema", "tblm2", "tblm2", []cookWriteRoot{
		{"Msg", []string{"m2_open"}},
	}},
	{"../../../tables/scalars", "scalars", "scalardemo", []cookWriteRoot{
		{"SimState", []string{"scalars_full", "scalars_default", "scalars_edges"}},
	}},
}

// cookWriteStub: a root whose closure holds a construct the Java cook form
// does not carry, so cook.go emits <Root>Cook as the STUB (a private
// constructor, no method). The conformance driver answers these rows absent
// on exactly that evidence, and cook_absent_is_structural pins it here. It is
// empty since the cook form carries unions (§7.2): every union root moved to
// cookWriteCarried.
var cookWriteStub = []cookWriteUnit{}

// cookWriteNoForm: a unit for which the generator emits no cook form at all:
// no <Root>Cook and no TableCookInfo. The driver answers these rows absent on
// that evidence. It is empty since the cook form carries the wide kinds (§15,
// §7.2): the wide-kind unit moved to cookWriteCarried.
var cookWriteNoForm = []cookWriteUnit{}

type cookManifest struct {
	instance map[string][3]string // row -> unit, root, wire path (repo-relative)
	cook     map[string][2]string // row -> little, big (repo-relative)
	order    []string             // cook-write rows in manifest order
}

func readCookManifest(t *testing.T) cookManifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	m := cookManifest{instance: map[string][3]string{}, cook: map[string][2]string{}}
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "instance" && len(f) >= 5:
			m.instance[f[1]] = [3]string{f[2], f[3], f[4]}
		case f[0] == "cook-write" && len(f) == 4:
			if _, dup := m.cook[f[1]]; dup {
				t.Fatalf("MANIFEST.txt names cook-write %s twice", f[1])
			}
			m.cook[f[1]] = [2]string{f[2], f[3]}
			m.order = append(m.order, f[1])
		}
	}
	if len(m.order) == 0 {
		t.Fatal("MANIFEST.txt holds no cook-write row: this test would pass over an empty set")
	}
	return m
}

func cookWritePartition(t *testing.T, m cookManifest) {
	t.Helper()
	seen := map[string]string{}
	count := map[string]int{}
	for list, units := range map[string][]cookWriteUnit{"carried": cookWriteCarried, "stub": cookWriteStub, "no-cook-form": cookWriteNoForm} {
		for _, u := range units {
			for _, r := range u.roots {
				for _, row := range r.rows {
					if prev, dup := seen[row]; dup {
						t.Errorf("%s is listed twice (%s and %s)", row, prev, list)
					}
					seen[row] = list
					count[list]++
					if _, ok := m.cook[row]; !ok {
						t.Errorf("%s (%s) is no cook-write row of MANIFEST.txt", row, list)
					}
					inst, ok := m.instance[row]
					if !ok {
						t.Errorf("%s has no instance row in MANIFEST.txt", row)
					} else if inst[0] != u.unit || inst[1] != r.root {
						t.Errorf("%s: MANIFEST.txt says %s %s, the %s list says %s %s", row, inst[0], inst[1], list, u.unit, r.root)
					}
				}
			}
		}
	}
	for _, row := range m.order {
		if _, ok := seen[row]; !ok {
			t.Errorf("cook-write %s has no Java answer: add it to cookWriteCarried, or name why it is absent", row)
		}
	}
	t.Logf("cook-write rows: %d in the manifest, %d carried, %d absent (stub, a union in the closure), %d absent (no cook form)",
		len(m.order), count["carried"], count["stub"], count["no-cook-form"])
}

// cookUnitFiles generates one unit for java, from a directory or from one
// .schema file (copied alone into a directory so unitFromDir reads only it).
func cookUnitFiles(t *testing.T, schema string) map[string][]byte {
	t.Helper()
	dir := schema
	if strings.HasSuffix(schema, ".schema") {
		dir = t.TempDir()
		b, err := os.ReadFile(schema)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(schema)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u := unitFromDir(t, dir)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate %s: %v", schema, err)
	}
	// the PACKET emitter's half rides beside the table half, as the java
	// target assembles them (compiler/target_java.go): the wide-kind unit's
	// Int128 and UInt128 are its files
	packet, err := javapkt.Generate(u)
	if err != nil {
		t.Fatalf("generate %s (packet half): %v", schema, err)
	}
	for name, body := range packet {
		if _, dup := files[name]; !dup {
			files[name] = body
		}
	}
	return files
}

func cookWriteAbsenceIsStructural(t *testing.T) {
	t.Helper()
	for _, u := range cookWriteStub {
		files := cookUnitFiles(t, u.schema)
		if _, ok := files["TableCookInfo.java"]; !ok {
			t.Errorf("%s: no TableCookInfo.java, so its stub rows would be absent for the wrong reason", u.pkg)
		}
		for _, r := range u.roots {
			src, ok := files[r.root+"Cook.java"]
			if !ok {
				t.Errorf("%s.%sCook.java is not emitted, where the stub is expected (rows %v)", u.pkg, r.root, r.rows)
				continue
			}
			for _, name := range []string{"cookMeasure(", "cookMeasureReason(", "cookFrom(", "open("} {
				if strings.Contains(string(src), name) {
					t.Errorf("%s.%sCook now declares %s: it is no stub, so rows %v belong in cookWriteCarried", u.pkg, r.root, name, r.rows)
				}
			}
		}
	}
	for _, u := range cookWriteNoForm {
		files := cookUnitFiles(t, u.schema)
		if _, ok := files["TableCookInfo.java"]; ok {
			t.Errorf("%s now emits a cook form (TableCookInfo.java): its rows belong in cookWriteCarried or cookWriteStub", u.pkg)
		}
		for _, r := range u.roots {
			if _, ok := files[r.root+"Cook.java"]; ok {
				t.Errorf("%s.%sCook.java is emitted: rows %v are no longer a unit without a cook form", u.pkg, r.root, r.rows)
			}
		}
	}
}

// compileCookUnit writes one generated unit and the given drivers into a
// temporary classpath and compiles them together.
func compileCookUnit(t *testing.T, javac string, files map[string][]byte, pkg string, drivers map[string]string) string {
	t.Helper()
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
	for name, body := range drivers {
		path := filepath.Join(dir, name+".java")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return dir
}

func cookDriverSource(class, pkg, root string) string {
	return strings.NewReplacer("COOKCLASS", class, "PKG", pkg, "ROOT", root).Replace(cookGoldenDriver)
}

// cookGoldenUnit compiles one carried unit once, then loads every row's
// committed table wire and writes the two schema cook reference images
// (§7.6), including the refusal paths that must not touch the output.
func cookGoldenUnit(t *testing.T, m cookManifest, u cookWriteUnit) {
	t.Helper()
	javac, javaBin := javaTools(t)
	drivers := map[string]string{}
	for _, r := range u.roots {
		drivers["CookGolden"+r.root] = cookDriverSource("CookGolden"+r.root, u.pkg, r.root)
	}
	dir := compileCookUnit(t, javac, cookUnitFiles(t, u.schema), u.pkg, drivers)
	repo := filepath.Join("..", "..", "..")
	for _, r := range u.roots {
		for _, row := range r.rows {
			t.Run(row, func(t *testing.T) {
				inst, cook := m.instance[row], m.cook[row]
				cmd := exec.Command(javaBin, "-ea", "-cp", dir, "CookGolden"+r.root,
					filepath.Join(repo, inst[2]), filepath.Join(repo, cook[0]), filepath.Join(repo, cook[1]))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("java cook %s: %v\n%s", row, err, out)
				}
			})
		}
	}
}

// cookRefusal pins §7.6's refusal by NAME: a value the cook cannot hold
// measures -1, cookMeasureReason answers "invalid_value", and cookFrom refuses
// in both orders without writing a byte. The value is graph_tree's Scene with
// one Layer past the declared bound of `layers [..MaxLayers]Layer`.
func cookRefusal(t *testing.T, m cookManifest) {
	t.Helper()
	javac, javaBin := javaTools(t)
	dir := compileCookUnit(t, javac, cookUnitFiles(t, "../../../tables/pointers"), "graphdemo",
		map[string]string{"CookRefusalDriver": cookRefusalDriver})
	cmd := exec.Command(javaBin, "-ea", "-cp", dir, "CookRefusalDriver",
		filepath.Join("..", "..", "..", m.instance["graph_tree"][2]))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java cook refusal: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "refused invalid_value layers") {
		t.Fatalf("the refusal driver did not reach the bound check:\n%s", out)
	}
}

const cookGoldenDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class COOKCLASS {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Path.of(args[0]));
        PKG.TableVariable.Report report = new PKG.TableVariable.Report();
        PKG.ROOTVariable.Value value = PKG.ROOTVariable.load(wire, report);
        if (value == null || report.refused || report.malformed) throw new AssertionError("wire load");
        long n = PKG.ROOTCook.cookMeasure(value);
        PKG.ROOTCook.MeasureReason reason = PKG.ROOTCook.cookMeasureReason(value);
        if (n < 0 || reason.bytes() != n || !reason.reason().equals("ok")) throw new AssertionError("measure");
        // a null root is no value: refused by name, and nothing written
        PKG.ROOTCook.MeasureReason none = PKG.ROOTCook.cookMeasureReason(null);
        if (PKG.ROOTCook.cookMeasure(null) != -1 || none.bytes() != -1 || !none.reason().equals("invalid_value")) {
            throw new AssertionError("null root measured " + none);
        }
        byte[] out = new byte[(int) n];
        Arrays.fill(out, (byte) 0x5a);
        if (PKG.ROOTCook.cookFrom(null, out, 1)) throw new AssertionError("null root accepted");
        if (PKG.ROOTCook.cookFrom(value, new byte[(int) n - 1], 1)) throw new AssertionError("short accepted");
        if (PKG.ROOTCook.cookFrom(value, out, 3)) throw new AssertionError("bad order accepted");
        for (byte b : out) if (b != (byte) 0x5a) throw new AssertionError("refusal wrote bytes");
        for (int order = 1; order <= 2; order++) {
            if (!PKG.ROOTCook.cookFrom(value, out, order)) throw new AssertionError("cook order " + order);
            byte[] want = Files.readAllBytes(Path.of(args[order]));
            if (!Arrays.equals(out, want)) {
                int at = Arrays.mismatch(out, want);
                throw new AssertionError("cook bytes order " + order + " first mismatch " + at +
                        " got " + (at < out.length ? (out[at] & 255) : -1) +
                        " want " + (at < want.length ? (want[at] & 255) : -1));
            }
        }
    }
}
`

const cookRefusalDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class CookRefusalDriver {
    public static void main(String[] args) throws Exception {
        graphdemo.TableVariable.Report report = new graphdemo.TableVariable.Report();
        graphdemo.SceneVariable.Value value = graphdemo.SceneVariable.load(Files.readAllBytes(Path.of(args[0])), report);
        if (value == null || report.refused || report.malformed) throw new AssertionError("wire load");
        long valid = graphdemo.SceneCook.cookMeasure(value);
        if (valid < 1) throw new AssertionError("graph_tree did not measure");
        int bound = -1;
        for (graphdemo.TableCookFieldInfo f : graphdemo.SceneCook.type().fields) {
            if (f.sourceName.equals("layers")) bound = f.arrayBound;
        }
        if (bound < 1) throw new AssertionError("no layers bound in the descriptors");
        while (value.layers.size() <= bound) value.layers.add(new graphdemo.LayerVariable.Value());
        long n = graphdemo.SceneCook.cookMeasure(value);
        graphdemo.SceneCook.MeasureReason reason = graphdemo.SceneCook.cookMeasureReason(value);
        if (n != -1 || reason.bytes() != -1 || !reason.reason().equals("invalid_value")) {
            throw new AssertionError("layers past the bound measured " + n + " " + reason);
        }
        byte[] out = new byte[(int) valid * 2];
        Arrays.fill(out, (byte) 0x5a);
        for (int order = 1; order <= 2; order++) {
            if (graphdemo.SceneCook.cookFrom(value, out, order)) throw new AssertionError("order " + order + " accepted");
        }
        for (byte b : out) if (b != (byte) 0x5a) throw new AssertionError("refusal wrote bytes");
        System.out.println("refused invalid_value layers " + value.layers.size() + " > " + bound);
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
