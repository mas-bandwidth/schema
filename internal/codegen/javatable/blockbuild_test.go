package javatable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestBlockBuildAndCookWriteJava is the DONE-WHEN gate of the
// "java-builds-blocks-and-cooks-in-the-runtime" card (docs/roadmap.sexp
// vt/block-build/java, vt/cook-write/java).
//
// BLOCK: it builds the two committed block images of testdata/conformance
// tables/MANIFEST.txt — block_render (unit blockdemo, tables/block/Render.schema)
// and block_padded (unit blockdemo, tables/block/Padded.schema) — with the
// Java BLOCK BUILDER (§19.1) and asserts the bytes equal the committed images,
// which the C++ producer test/tables/block_main.cpp wrote. block_padded is
// compared whole; block_render reproduces every fill_camera..fill_explosion
// value from the independent C++ producer and compares the complete image.
//
// COOK: it cooks the same data with the Java COOK WRITE (§7.6) in each byte
// order and asserts the bytes equal the tooling cook oracle in both orders.
// Short buffers and invalid orders must refuse without changing output.
func TestBlockBuildAndCookWriteJava(t *testing.T) {
	t.Parallel()

	javac, javaBin := javaTools(t)

	t.Run("blockBuild", func(t *testing.T) {
		t.Parallel()

		u := blockdemoUnit(t)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		classes := filepath.Join(dir, "classes")
		pkgdir := filepath.Join(src, "blockdemo")
		if err := os.MkdirAll(pkgdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(classes, 0o755); err != nil {
			t.Fatal(err)
		}
		srcs := []string{}
		for name, data := range files {
			path := filepath.Join(pkgdir, name)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			srcs = append(srcs, path)
		}
		if err := os.WriteFile(filepath.Join(src, "Driver.java"), []byte(blockBuildDriver), 0o644); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"--release", "17", "-d", classes}, srcs...)
		args = append(args, filepath.Join(src, "Driver.java"))
		if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac: %v\n%s", err, out)
		}
		out, err := exec.Command(javaBin, "-ea", "-cp", classes, "Driver", dir).CombinedOutput()
		if err != nil {
			t.Fatalf("java: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "ok") {
			t.Fatalf("driver: %s", out)
		}

		// block_padded: the WHOLE image, byte-equal to the committed golden the
		// C++ producer wrote (test/tables/block_main.cpp, pin_block_golden).
		builtPadded, err := os.ReadFile(filepath.Join(dir, "block_padded.build.bin"))
		if err != nil {
			t.Fatal(err)
		}
		wantPadded, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "block_padded.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(builtPadded, wantPadded) {
			t.Errorf("block_padded: %d bytes built, %d committed, equal=%v",
				len(builtPadded), len(wantPadded), bytes.Equal(builtPadded, wantPadded))
			for i := range min(len(builtPadded), len(wantPadded)) {
				if builtPadded[i] != wantPadded[i] {
					t.Fatalf("first differing byte at %d: built %02x, committed %02x", i, builtPadded[i], wantPadded[i])
				}
			}
		}

		// Both whole images must match the independent C++ producer.
		builtRender, err := os.ReadFile(filepath.Join(dir, "block_render.build.bin"))
		if err != nil {
			t.Fatal(err)
		}
		wantRender, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "block_render.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(builtRender, wantRender) {
			for i := range min(len(builtRender), len(wantRender)) {
				if builtRender[i] != wantRender[i] {
					t.Fatalf("block_render byte %d: built %02x, committed %02x", i, builtRender[i], wantRender[i])
				}
			}
			t.Fatalf("block_render length: built %d, committed %d", len(builtRender), len(wantRender))
		}
	})

	t.Run("cookWriteLittle", func(t *testing.T) {
		t.Parallel()
		testCookWriteJava(t, false)
	})
	t.Run("cookWriteBig", func(t *testing.T) {
		t.Parallel()
		testCookWriteJava(t, true)
	})
}

// blockdemoUnit parses and checks the two-file blockdemo unit of tables/block.
func blockdemoUnit(t *testing.T) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	for _, name := range []string{"Render.schema", "Padded.schema"} {
		path := filepath.Join("..", "..", "..", "test", "tables", "block", name)
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			path = filepath.Join("..", "..", "..", "tables", "block", name)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(name, src)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", name, perrs[0])
		}
		base := strings.TrimSuffix(name, ".schema")
		sources = append(sources, check.SourceFile{Path: name, Name: name, Base: base, Bytes: src, AST: f})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check blockdemo: %v", cerrs[0])
	}
	return u
}

// blockBuildDriver builds both committed block images with the Java builder
// (§19.1) and writes them for the Go side to compare. The values are
// block_main.cpp's: golden_counts() and the padded golden's fill.
const blockBuildDriver = `import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class Driver {
    private Driver() {}

    static void putI64(byte[] d, int at, long v) { for (int i = 0; i < 8; i++) { d[at + i] = (byte) (v >>> (i * 8)); } }
    static void putI32(byte[] d, int at, int v) { for (int i = 0; i < 4; i++) { d[at + i] = (byte) (v >>> (i * 8)); } }
    static void putI16(byte[] d, int at, int v) { d[at] = (byte) v; d[at + 1] = (byte) (v >>> 8); }

    static blockdemo.TableBlockFieldInfo field(blockdemo.TableBlockInfo info, String name) {
        for (blockdemo.TableBlockFieldInfo f : info.fields) {
            if (f.name.equals(name)) { return f; }
        }
        throw new IllegalStateException("no field " + name);
    }

    // Reproduce block_main.cpp's fill functions through the emitted descriptors.
    static void scalar(byte[] d, int at, blockdemo.TableBlockInfo info, String name, double value) {
        blockdemo.TableBlockFieldInfo f = field(info, name);
        long bits = f.kind == 10 ? Float.floatToRawIntBits((float) value) : f.kind == 11 ? Double.doubleToRawLongBits(value) : (long) value;
        for (int j = 0; j < f.size; j++) { d[at + f.offset + j] = (byte) (bits >>> (8 * j)); }
    }
    static void vector(byte[] d, int at, blockdemo.TableBlockInfo info, String name, int salt, int i, boolean quat) {
        blockdemo.TableBlockFieldInfo f = field(info, name);
        blockdemo.TableBlockInfo t = f.element();
        scalar(d, at + f.offset, t, "x", (quat ? 0.25 : 1.5) * i + salt);
        scalar(d, at + f.offset, t, "y", quat ? 0.125 * i : 2.25 * i - salt);
        scalar(d, at + f.offset, t, "z", quat ? 0.0625 * i - salt : 0.5 * i + 2 * salt);
        if (quat) { scalar(d, at + f.offset, t, "w", 1.0 - 0.5 * (i % 3)); }
    }
    static void fillRender(byte[] d, blockdemo.TableBlockInfo projection) {
        String[] names = {"cameras", "ships", "turrets", "missiles", "dynamic_props", "static_props", "cosmetic_props", "lasers", "explosions"};
        int[] counts = {1, 5, 7, 3, 2, 4, 3, 6, 2};
        for (int a = 0; a < names.length; a++) {
            blockdemo.TableBlockFieldInfo array = field(projection, names[a]);
            int start = (int) blockdemo.TableBytes.i64(d, array.offsetOfOffset);
            blockdemo.TableBlockInfo row = array.element();
            for (int i = 0; i < counts[a]; i++) {
                int at = start + i * array.stride;
                if (a == 7) { vector(d, at, row, "start", 8, i, false); vector(d, at, row, "finish", 9, i, false); }
                else { if (a != 2) vector(d, at, row, "position", a == 8 ? 10 : a + 1, i, false); vector(d, at, row, "rotation", a == 8 ? 10 : a + 1, i, true); }
                switch (a) {
                    case 0: scalar(d,at,row,"camera_id",i*7+1); scalar(d,at,row,"camera_type",i%4); scalar(d,at,row,"target_object_id",i*13+2); scalar(d,at,row,"fov",0.5*i+60); break;
                    case 1: scalar(d,at,row,"flags",i%16); scalar(d,at,row,"object_id",i*3+11); scalar(d,at,row,"target_object_id",i*5+7); scalar(d,at,row,"thrust",0.25*i); scalar(d,at,row,"object_sequence",i%251); scalar(d,at,row,"ship_type",i%4); scalar(d,at,row,"has_target_lock",i%2==0?1:0); scalar(d,at,row,"predicted_explode",i%3==0?1:0); break;
                    case 2: scalar(d,at,row,"flags",i*17); scalar(d,at,row,"object_id",i*2+1); scalar(d,at,row,"parent_object_id",i/3); scalar(d,at,row,"turret_index",i%8); scalar(d,at,row,"target_object_id",i*11); scalar(d,at,row,"object_sequence",i%253); scalar(d,at,row,"has_target_lock",i%5==0?1:0); break;
                    case 3: scalar(d,at,row,"flags",i*19); scalar(d,at,row,"object_id",i*23); scalar(d,at,row,"object_sequence",i%249); scalar(d,at,row,"missile_type",i%3); break;
                    case 4: scalar(d,at,row,"flags",i*29); scalar(d,at,row,"object_id",i*31); scalar(d,at,row,"object_sequence",i%247); scalar(d,at,row,"prop_type",i%4); break;
                    case 5: scalar(d,at,row,"scale",0.5+0.25*(i%7)); scalar(d,at,row,"flags",i*37); scalar(d,at,row,"static_prop_id",i*41); scalar(d,at,row,"prop_type",i%4); break;
                    case 6: scalar(d,at,row,"scale",0.25+0.125*(i%5)); scalar(d,at,row,"flags",i*43); scalar(d,at,row,"cosmetic_prop_id",i*47); scalar(d,at,row,"prop_sequence",i%241); scalar(d,at,row,"prop_type",i%4); break;
                    case 7: scalar(d,at,row,"t",0.125*(i%8)); scalar(d,at,row,"laser_id",i*53); scalar(d,at,row,"laser_type",i%3); break;
                    case 8: scalar(d,at,row,"t",0.0625*(i%16)); scalar(d,at,row,"explosion_id",i*59); scalar(d,at,row,"parent_object_id",i*61); scalar(d,at,row,"explosion_type",i%3); break;
                    default: throw new IllegalStateException();
                }
                if (a != 0) scalar(d,at,row,"team",i%5);
            }
        }
    }

    public static void main(String[] args) throws Exception {
        blockdemo.TableBlockRefusal refusal = new blockdemo.TableBlockRefusal();

        // ---- block_render: Begin + the projection's own field, golden_counts() ----
        blockdemo.RenderFrameBlock.Storage rs = new blockdemo.RenderFrameBlock.Storage();
        if (!rs.create()) { throw new IllegalStateException("render storage"); }
        blockdemo.RenderFrameBlock render = blockdemo.RenderFrameBlock.begin(rs,
                new blockdemo.RenderFrameCounts(1, 5, 7, 3, 2, 4, 3, 6, 2), refusal);
        if (render == null) { throw new IllegalStateException("render begin: " + refusal.array); }
        byte[] rdata = rs.data();
        blockdemo.TableBlockFieldInfo version = field(blockdemo.RenderFrameBlock.type(), "version");
        putI64(rdata, version.offset, 1L); // RenderVersion
        fillRender(rdata, blockdemo.RenderFrameBlock.type());
        if (blockdemo.RenderFrameBlock.open(rdata, 0, render.blockBytes()) == null) { throw new IllegalStateException("built render open"); }
        Files.write(Paths.get(args[0], "block_render.build.bin"),
                Arrays.copyOf(rdata, (int) render.blockBytes()));
        if (render.blockBytes() != render.bytes()) { throw new IllegalStateException("render bytes disagree"); }

        // ---- block_padded: the whole image, the padded golden's values ----
        blockdemo.PaddedFrameBlock.Storage ps = new blockdemo.PaddedFrameBlock.Storage();
        if (!ps.create()) { throw new IllegalStateException("padded storage"); }
        blockdemo.PaddedFrameBlock padded = blockdemo.PaddedFrameBlock.begin(ps,
                new blockdemo.PaddedFrameCounts(4), refusal);
        if (padded == null) { throw new IllegalStateException("padded begin: " + refusal.array); }
        byte[] pdata = ps.data();
        blockdemo.TableBlockInfo proj = blockdemo.PaddedFrameBlock.type();
        pdata[field(proj, "marker").offset] = 7;
        putI64(pdata, field(proj, "stamp").offset, 0x0123456789abcdefL);
        blockdemo.TableBlockFieldInfo blob = field(proj, "blob");
        byte[] twelve = "twelve bytes".getBytes(StandardCharsets.US_ASCII);
        System.arraycopy(twelve, 0, pdata, blob.offset, 12);
        putI32(pdata, blob.countOffset, 12);
        blockdemo.TableBlockInfo row = blockdemo.PaddedRowRow.blockInfo();
        for (int i = 0; i < 4; i++) {
            int at = padded.rowsAt(i);
            pdata[at + field(row, "tag").offset] = (byte) (10 + i);
            putI64(pdata, at + field(row, "value").offset, Double.doubleToLongBits(0.5 * i + 100.0));
            pdata[at + field(row, "flag").offset] = (byte) ((i % 2 == 0) ? 1 : 0);
            putI32(pdata, at + field(row, "id").offset, i * 1000 + 3);
            blockdemo.TableBlockFieldInfo label = field(row, "label");
            byte[] text = ("row-" + i).getBytes(StandardCharsets.US_ASCII);
            System.arraycopy(text, 0, pdata, at + label.offset, text.length);
            putI32(pdata, at + label.countOffset, text.length);
            blockdemo.TableBlockFieldInfo slots = field(row, "slots");
            for (int s = 0; s < 4; s++) { putI16(pdata, at + slots.offset + s * slots.elemSize, i * 4 + s); }
            blockdemo.TableBlockFieldInfo teams = field(row, "teams");
            for (int s = 0; s < 4; s++) { pdata[at + teams.offset + s * teams.elemSize] = (byte) (i + s + 1); }
            blockdemo.TableBlockFieldInfo counter = field(row, "counter");
            putI32(pdata, at + counter.offset, i * 9);
            pdata[at + counter.presentOffset] = (byte) ((i % 2 == 1) ? 1 : 0);
        }
        Files.write(Paths.get(args[0], "block_padded.build.bin"), Arrays.copyOf(pdata, (int) padded.blockBytes()));
        System.out.println("ok");
    }
}
`

func testCookWriteJava(t *testing.T, big bool) {
	t.Helper()
	u := unitFrom(t, `package probe
fixed table Probe {
 number int64
 ratio float32
 label string(8)
 blob bytes(4)
 marks [..4]int32
}`)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Probe"])
	report := new(tabletext.Report)
	if !m.Read(v, []byte(`{"number":-7,"ratio":1.5,"label":"hello","blob":"AQID","marks":[4,5]}`), report) {
		t.Fatalf("oracle JSON: %+v", report)
	}
	want, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	order := 1
	if big {
		order = 2
	}
	driver := fmt.Sprintf(`package probe;
public final class Driver {
 public static void main(String[] args) throws Exception {
  ProbeFixed.Value v=new ProbeFixed.Value();v.number=-7;v.ratio=1.5f;
  byte[] text="hello".getBytes(java.nio.charset.StandardCharsets.UTF_8);System.arraycopy(text,0,v.label,0,5);v.labelLength=5;
  v.blob[0]=1;v.blob[1]=2;v.blob[2]=3;v.blobLength=3;
  v.marks[0]=4;v.marks[1]=5;v.marksCount=2;
  long size=ProbeCook.cookMeasure(v);
  if(size<0 || ProbeCook.cookMeasureReason(v).reason()!=null)throw new AssertionError("measure");
  byte[] output=new byte[(int)size];
  if(!ProbeCook.cookFrom(v,output,%d))throw new AssertionError("cookFrom");
  byte[] shortBuffer=new byte[(int)size-1];java.util.Arrays.fill(shortBuffer,(byte)0x55);
  if(ProbeCook.cookFrom(v,shortBuffer,1))throw new AssertionError("short accepted");
  for(byte b:shortBuffer)if(b!=0x55)throw new AssertionError("short mutated");
  byte[] invalid=new byte[(int)size];java.util.Arrays.fill(invalid,(byte)0x66);
  if(ProbeCook.cookFrom(v,invalid,3))throw new AssertionError("order accepted");
  for(byte b:invalid)if(b!=0x66)throw new AssertionError("invalid mutated");
  v.marksCount=5;if(!"invalid_value".equals(ProbeCook.cookMeasureReason(v).reason()))throw new AssertionError("invalid_value reason");
  java.nio.file.Files.write(java.nio.file.Paths.get(args[0]),output);
 }
}`, order)
	dp := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(dp, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dp)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	path := filepath.Join(dir, "cook.bin")
	if out, err := exec.Command(java, "-ea", "-cp", dir, "probe.Driver", path).CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		for i := range min(len(got), len(want)) {
			if got[i] != want[i] {
				t.Fatalf("cook order big=%v byte %d: got %02x want %02x", big, i, got[i], want[i])
			}
		}
		t.Fatalf("cook sizes %d != %d", len(got), len(want))
	}
}

func TestCookWriteJavaSharedGraph(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, `package probe
fixed table Meta {
 build int32 = 1
 tag string(8)
}
table Node {
 value int32
 next *Node
 text *string
}
table Root {
 first *Node
 second *Node
 meta Meta
 values []int32
 children []*Node
 metas []Meta
 rows [..2]Meta
 padding []uint8
 empty []int64
 tail []uint8
}
`)
	text := []byte(`{"first":{"&node":1,"value":7,"next":null,"text":"shared"},"second":{"&node":1},"meta":{"build":9,"tag":"meta"},"values":[1,-2,3],"children":[{"&node":1}],"metas":[{"build":11,"tag":"row😀"}],"rows":[],"padding":[1],"empty":[],"tail":[2]}`)
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Root"])
	rep := new(tabletext.Report)
	if !m.Read(v, text, rep) {
		t.Fatalf("oracle: %+v", rep)
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.json"), text, 0o644); err != nil {
		t.Fatal(err)
	}
	driver := `package probe;
public final class Driver {
 public static void main(String[] args) throws Exception {
  byte[] json=java.nio.file.Files.readAllBytes(java.nio.file.Paths.get(args[0],"input.json"));
  TableVariable.Report report=new TableVariable.Report();RootVariable.Value v=RootVariable.fromJson(json,report);
  if(v==null||report.malformed||v.first!=v.second)throw new AssertionError("load sharing");
  long size=RootCook.cookMeasure(v);if(size<0)throw new AssertionError("measure");
  for(int order=1;order<=2;order++) {byte[] b=new byte[(int)size];if(!RootCook.cookFrom(v,b,order))throw new AssertionError("cook");java.nio.file.Files.write(java.nio.file.Paths.get(args[0],"cook"+order),b);}
  v.first.next=v.first;
  if(RootCook.cookMeasure(v)!=-1||!"invalid_value".equals(RootCook.cookMeasureReason(v).reason()))throw new AssertionError("cycle accepted");
  byte[] bad=new byte[(int)size];java.util.Arrays.fill(bad,(byte)0x55);
  if(RootCook.cookFrom(v,bad,1))throw new AssertionError("cycle write accepted");
  for(byte b:bad)if(b!=0x55)throw new AssertionError("cycle mutated buffer");
 }
}`
	dp := filepath.Join(dir, "Driver.java")
	if err := os.WriteFile(dp, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, dp)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", dir}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	if out, err := exec.Command(java, "-ea", "-cp", dir, "probe.Driver", dir).CombinedOutput(); err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	for order := 1; order <= 2; order++ {
		want, err := tablecook.Cook(m, v, tablecook.Options{Big: order == 2})
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("cook%d", order)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			for i := range min(len(got), len(want)) {
				if got[i] != want[i] {
					t.Fatalf("order %d byte %d: got %02x want %02x", order, i, got[i], want[i])
				}
			}
			t.Fatalf("order %d lengths %d/%d", order, len(got), len(want))
		}
	}
}
