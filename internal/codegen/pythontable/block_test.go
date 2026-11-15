package pythontable

// THE BLOCK FORM'S READ HALF on the Python leg (docs/SPEC-TABLES.md §19.2,
// §19.3). The block images are the Go builder's own fixtures from the tables
// manifest, and the expected text is the canonical ROW DUMP the Go descriptors
// produce (testdata/conformance/tables/FORMAT.md), so this leg reads every
// field of every row through the generated descriptors and is compared value
// for value with Go. A damaged image refuses under the same TableRefuseReason
// name a Go Open reports (§7, §19.2).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestPythonBlockRead(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	rows := pyBlockManifestRows(t, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if len(rows) == 0 {
		t.Fatal("the manifest names no block image")
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			image, err := os.ReadFile(filepath.Join(repo, row.wire))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join(repo, row.dump))
			if err != nil {
				t.Fatal(err)
			}
			root := pyBlockRoot(t, want)
			probe := pyrun.ImportPrelude + fmt.Sprintf(`import blockdemo
from blockdemo._schema import block as B

root = %q
version = blockdemo.BUILD_VERSION
data = open("block", "rb").read()
handle = B.BlockOpen(blockdemo, root, data, version)
import sys
sys.stdout.write(B.Dump(handle))
`, root)
			current := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image}))
			floor := string(pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image}))
			if current != floor {
				t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", current, floor)
			}
			if current != string(want) {
				t.Errorf("%s: the reflective read differs from Go's dump:\n--- python ---\n%s\n--- go ---\n%s", row.name, current, want)
			}
		})
	}
	if out, err := pyrun.Analyze(t, func() string { dir := t.TempDir(); writeTree(t, dir, files); return dir }(), "blockdemo"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
}

// TestPythonBlockReadNativeOrder holds the runtime to the host's own order:
// the producer stamps the order it wrote in (§19.1), so the read is native on
// every host and never a little-endian assumption.
func TestPythonBlockReadNativeOrder(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	image, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `import sys
import blockdemo
from blockdemo._schema import block as B

host = 1 if sys.byteorder == "little" else 2
if B.BYTE_ORDER != host:
    raise SystemExit("the block runtime's byte order is not this host's")
data = open("block", "rb").read()
handle = B.BlockOpen(blockdemo, "RenderFrame", data, blockdemo.BUILD_VERSION)
print("native-order-ok", len(B.Dump(handle)) > 0)
`
	out := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image}))
	if out != "native-order-ok True\n" {
		t.Fatalf("native-order probe: %q", out)
	}
}

// TestPythonBlockReadOppositeOrder holds the one refusal the prologue exists
// for (§19.1, §19.2): a byte-reversed magic is a foreign order, and an intact
// magic with the other order word is not a block at all.
func TestPythonBlockReadOppositeOrder(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	image, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + fmt.Sprintf(`import sys
import blockdemo
from blockdemo import Refused
from blockdemo._schema import block as B

MAGIC = 0x4b4c42414d484353
version = 0x%016x
base = open("block", "rb").read()
other = "big" if sys.byteorder == "little" else "little"
foreign = MAGIC.to_bytes(8, other) + base[8:]
try:
    B.BlockOpen(blockdemo, "RenderFrame", foreign, version)
except Refused as err:
    assert err.reason == "foreign_order", "opposite order: want foreign_order, got " + err.reason
else:
    raise SystemExit("accepted a block in the opposite order")
wrong_order = 2 if sys.byteorder == "little" else 1
wrong = base[:16] + wrong_order.to_bytes(8, sys.byteorder) + base[24:]
try:
    B.BlockOpen(blockdemo, "RenderFrame", wrong, version)
except Refused as err:
    assert err.reason == "not_a_cook", "wrong order word: want not_a_cook, got " + err.reason
else:
    raise SystemExit("accepted a block whose byte order word disagrees")
print("opposite-order-ok")
`, ir.BuildVersion(u))
	out := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image}))
	if out != "opposite-order-ok\n" {
		t.Fatalf("opposite-order probe: %q", out)
	}
}

// TestPythonBlockReadDamaged is §19.2's refusal battery, first failing clause
// first: the prologue, the pitch, the count, the array start and its extent,
// the bytes the caller passed, and last the base's alignment.
func TestPythonBlockReadDamaged(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	image, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + fmt.Sprintf(`import sys
import blockdemo
from blockdemo import Refused
from blockdemo._schema import block as B

version = 0x%016x
base = open("block", "rb").read()

def patch(data, off, width, value):
    out = bytearray(data)
    out[off:off + width] = int(value).to_bytes(width, sys.byteorder)
    return bytes(out)

def expect(reason, data, lead=0):
    try:
        B.BlockOpen(blockdemo, "RenderFrame", data, version, lead)
    except Refused as err:
        if err.reason != reason:
            raise SystemExit("want " + reason + ", got " + err.reason)
        print("refuse", reason)
        return
    raise SystemExit("accepted a damaged block: wanted " + reason)

cameras = next(f for f in blockdemo.RenderFrame.block.projection.fields if f.name == "cameras")
expect("truncated", base[:23])
expect("not_a_cook", b"\0" * 8 + base[8:])
other = "big" if sys.byteorder == "little" else "little"
expect("foreign_order", (0x4b4c42414d484353).to_bytes(8, other) + base[8:])
expect("wrong_build_version", patch(base, 8, 8, version + 1))
host = 1 if sys.byteorder == "little" else 2
expect("not_a_cook", patch(base, 16, 8, 2 if host == 1 else 1))
expect("bad_layout", patch(base, cameras.offset + 12, 4, 0))
expect("bad_layout", patch(base, cameras.offset + 8, 4, cameras.bound + 1))
expect("bad_layout", patch(base, cameras.offset, 8, 0x7fffffffffffffc0))
expect("bad_layout", patch(base, cameras.offset + 8, 4, 1000))
expect("truncated", base[:-1])
expect("unaligned_base", base, 1)
print("block-refusals-ok")
`, ir.BuildVersion(u))
	out := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image}))
	if !strings.Contains(out, "block-refusals-ok\n") {
		t.Fatalf("damaged-block probe: %q", out)
	}
}

// TestPythonBlockReadNegativeControls plants one defect per clause into the
// emitted block runtime and requires the gate to go red: a float read one bit
// off (the dump), and the pitch, bound and base-alignment checks removed.
func TestPythonBlockReadNegativeControls(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	image, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "block_render.bin"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "block", "block_render.dump"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		anchor string
		repl   string
		mode   string
	}{
		{
			name:   "dump",
			anchor: `return "0x%08x" % _u(view, at, 4)`,
			repl:   `return "0x%08x" % (_u(view, at, 4) ^ 1)`,
			mode:   "dump",
		},
		{
			name:   "pitch",
			anchor: "if stride != a.stride:",
			repl:   "if False:",
			mode:   "refusals",
		},
		{
			name:   "bound",
			anchor: "if count > a.maximum:",
			repl:   "if False:",
			mode:   "refusals",
		},
		{
			name:   "unaligned",
			anchor: "if base_offset % BLOCK_ALIGN != 0:",
			repl:   "if False:",
			mode:   "refusals",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			pyBlockPatchOnce(t, files, u.Package+"/_schema/block.py", tc.anchor, tc.repl)
			dir := t.TempDir()
			writeTree(t, dir, files)
			if tc.mode == "refusals" {
				probe := pyrun.ImportPrelude + fmt.Sprintf(`import sys
import blockdemo
from blockdemo import Refused
from blockdemo._schema import block as B

version = 0x%016x
base = open("block", "rb").read()

def patch(data, off, width, value):
    out = bytearray(data)
    out[off:off + width] = int(value).to_bytes(width, sys.byteorder)
    return bytes(out)

def expect(reason, data, lead=0):
    try:
        B.BlockOpen(blockdemo, "RenderFrame", data, version, lead)
    except Refused as err:
        if err.reason != reason:
            raise SystemExit("want " + reason + ", got " + err.reason)
        return
    raise SystemExit("accepted a damaged block: wanted " + reason)

cameras = next(f for f in blockdemo.RenderFrame.block.projection.fields if f.name == "cameras")
expect("bad_layout", patch(base, cameras.offset + 12, 4, 0))
expect("bad_layout", patch(base, cameras.offset + 8, 4, cameras.bound + 1))
expect("unaligned_base", base, 1)
print("block-refusals-ok")
`, ir.BuildVersion(u))
				_, stderr, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image})
				if err == nil {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s left the block refusal battery green", tc.name)
				}
				if !strings.Contains(stderr, "accepted a damaged block") && !strings.Contains(stderr, "want ") {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s went red, but not on the refusal:\n%s", tc.name, stderr)
				}
				return
			}
			root := pyBlockRoot(t, want)
			probe := pyrun.ImportPrelude + fmt.Sprintf(`import sys
import blockdemo
from blockdemo._schema import block as B

handle = B.BlockOpen(blockdemo, %q, open("block", "rb").read(), 0x%016x)
sys.stdout.write(B.Dump(handle))
`, root, ir.BuildVersion(u))
			got := pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "block": image})
			if string(got) == string(want) {
				t.Fatalf("NEGATIVE CONTROL FAILED: %s left the block dump identical to Go", tc.name)
			}
		})
	}
}

type pyBlockManifestRow struct {
	name string
	unit string
	wire string
	dump string
}

func pyBlockManifestRows(t *testing.T, path string) []pyBlockManifestRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []pyBlockManifestRow
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && f[0] == "block" {
			rows = append(rows, pyBlockManifestRow{name: f[1], unit: f[2], wire: f[3], dump: f[4]})
		}
	}
	return rows
}

func pyBlockRoot(t *testing.T, dump []byte) string {
	t.Helper()
	line, _, _ := strings.Cut(string(dump), "\n")
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "projection" {
		t.Fatalf("the dump's first line names no projection: %q", line)
	}
	return f[1]
}

// loadUnitFiles parses and checks every .schema file under a directory into
// one unit, as the other legs' run helpers do.
func loadUnitFiles(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") && len(e.Name()) > len(".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			ast, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			sources = append(sources, check.SourceFile{
				Path:  name,
				Name:  filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data,
				AST:   ast,
			})
		}
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// pyBlockPatchOnce replaces one anchor in a generated file, failing loudly when
// the anchor moved so a control cannot silently watch nothing.
func pyBlockPatchOnce(t *testing.T, files map[string][]byte, path, anchor, repl string) {
	t.Helper()
	text, ok := files[path]
	if !ok {
		t.Fatalf("the generated tree has no %s", path)
	}
	if !strings.Contains(string(text), anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	files[path] = []byte(strings.Replace(string(text), anchor, repl, 1))
}
