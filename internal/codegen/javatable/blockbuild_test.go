package javatable

import (
	"bytes"
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
// compared whole; block_render is compared over the projection — the prologue
// and the nine triples Begin writes, at §19.1's worked offsets — because the
// row bodies' fill (block_main.cpp's fill_camera..fill_explosion) is not
// reproduced on this leg yet.
//
// COOK: it cooks the same data with the Java COOK WRITE (§7.6) in each byte
// order and asserts the bytes equal `schema cook` in both orders. NOT
// IMPLEMENTED YET: the cook subtests stay red until <T>CookMeasure,
// <T>CookMeasureReason and <T>CookFrom land on this leg.
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

		// block_render: the PROJECTION — the prologue and the nine triples
		// Begin writes at §19.1's worked offsets — byte-equal to the committed
		// golden's projection.
		builtRender, err := os.ReadFile(filepath.Join(dir, "block_render.build.bin"))
		if err != nil {
			t.Fatal(err)
		}
		wantRender, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "block_render.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if len(builtRender) > len(wantRender) {
			t.Fatalf("block_render: projection %d bytes past the %d-byte golden", len(builtRender), len(wantRender))
		}
		if !bytes.Equal(builtRender, wantRender[:len(builtRender)]) {
			for i := range builtRender {
				if builtRender[i] != wantRender[i] {
					t.Fatalf("block_render projection: first differing byte at %d: built %02x, committed %02x",
						i, builtRender[i], wantRender[i])
				}
			}
		}
	})

	t.Run("cookWriteLittle", func(t *testing.T) {
		t.Parallel()
		t.Error("cook write (§7.6) is not implemented on the Java leg yet: <T>CookMeasure, <T>CookMeasureReason and <T>CookFrom are owed by this card")
	})
	t.Run("cookWriteBig", func(t *testing.T) {
		t.Parallel()
		t.Error("cook write (§7.6) is not implemented on the Java leg yet: <T>CookMeasure, <T>CookMeasureReason and <T>CookFrom are owed by this card")
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
        Files.write(Paths.get(args[0], "block_render.build.bin"),
                Arrays.copyOf(rdata, blockdemo.RenderFrameBlock.projectionSize));
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
