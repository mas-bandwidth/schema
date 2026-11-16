package pythontable

// THE BLOCK FORM'S BUILD HALF on the Python leg (docs/SPEC-TABLES.md §19.1).
// The Go builder is the oracle: one 64-byte aligned extent whose projection
// opens with the three-word prologue and carries, per out-of-line array, the
// (offset_of, count, stride) triple. This test builds the blockdemo block roots
// in Python through _schema/blockbuild.py, compares every byte against the
// generated Go builder, and opens the Python bytes with the generated Go
// reader.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonBlockBuilder(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go and the pinned python")
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the Python unit: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)

	roots := []string{"RenderFrame", "PaddedFrame", "PaddedRow"}
	probe := pyrun.ImportPrelude + fmt.Sprintf(`import blockdemo
from blockdemo._schema import blockbuild as BB
from blockdemo import InvalidValue

T = blockdemo
plan = [
    ("RenderFrame", {"cameras": 1, "ships": 3, "turrets": 2, "missiles": 1,
                     "dynamic_props": 2, "static_props": 4, "cosmetic_props": 1,
                     "lasers": 3, "explosions": 2}),
    ("PaddedFrame", {"rows": 3}),
    ("PaddedRow", {}),
]
for name, counts in plan:
    block = BB.Begin(T, name, counts)
    if BB.BlockBytes(block) != len(block.bytes):
        raise SystemExit("BlockBytes disagrees with the extent")
    print(bytes(block.bytes).hex())
try:
    BB.Begin(T, "RenderFrame", {"ships": 4097})
except InvalidValue as err:
    print("refused", err.path)
else:
    raise SystemExit("Begin accepted a count past the declared maximum")
`)
	probeFile := "main.py"
	if err := os.WriteFile(filepath.Join(dir, probeFile), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := pyBlockRunBoth(t, dir, probeFile, nil)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(roots)+1 {
		t.Fatalf("the Python probe returned %d lines, want %d:\n%s", len(lines), len(roots)+1, out)
	}
	for i, root := range roots {
		data, err := hexDecode(lines[i])
		if err != nil {
			t.Fatalf("%s: the probe line is not hex: %v", root, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "py_"+root+".bin"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := lines[len(roots)]; got != "refused ships" {
		t.Fatalf("the refusal names %q, want refused ships", got)
	}

	// The generated Go builder writes the oracle bytes and opens the Python ones.
	goFiles, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go unit: %v", err)
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("generate the Go block surface: %v", err)
	}
	maps.Copy(goFiles, tables)
	serialize := slowtest.SerializeGo(t, "../../../")
	goFiles["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", serialize))
	goFiles["block_test.go"] = []byte(pyBlockOpenProbe)
	for name, data := range goFiles {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("nice", "-n", "19", "go", "test", "-count=1", "-timeout", "600s", "-run", "^TestPythonBlockOpen$", ".")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated Go reader: %v\n%s", err, output)
	}

	for _, root := range roots {
		oracle, err := os.ReadFile(filepath.Join(dir, "go_"+root+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		python, err := os.ReadFile(filepath.Join(dir, "py_"+root+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(oracle, python) {
			t.Errorf("%s: the Python builder's %d bytes differ from the generated Go builder's %d", root, len(python), len(oracle))
		}
	}
}

// TestPythonBlockBuilderNegativeControls plants one defect per clause into the
// emitted blockbuild runtime and requires the builder probe to go red: the
// array start alignment removed, the count bound removed, and the 64-byte
// extent rounding removed.
func TestPythonBlockBuilderNegativeControls(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	u := loadUnitFiles(t, filepath.Join(repo, "tables", "block"))
	runProbe := func(t *testing.T, path func(testing.TB) string, patch func(map[string][]byte)) (string, error) {
		t.Helper()
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		if patch != nil {
			patch(files)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := pyrun.ImportPrelude + `import blockdemo
from blockdemo._schema import block as B, blockbuild as BB
from blockdemo import InvalidValue

T = blockdemo
b1 = BB.Begin(T, "PaddedFrame", {"rows": 3})
if len(b1.bytes) % 64 != 0:
    raise SystemExit("extent not 64-byte aligned")
if b1.arrays[0].offset % 64 != 0:
    raise SystemExit("array start not 64-byte aligned")
h = B.BlockOpen(T, "PaddedFrame", b1.bytes, T.BUILD_VERSION)
try:
    BB.Begin(T, "RenderFrame", {"ships": 4097})
except InvalidValue as err:
    if err.path != "ships":
        raise SystemExit("wrong invalid path")
else:
    raise SystemExit("over-count accepted")
print("ok")
`
		if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := pyrun.Try(t, path(t), dir, "main.py", nil)
		return strings.TrimSpace(out + stderr), err
	}
	for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
		if out, err := runProbe(t, pin, nil); err != nil || out != "ok" {
			t.Fatalf("unsabotaged blockbuild probe failed: %q %v", out, err)
		}
	}
	for _, tc := range []struct {
		name     string
		anchor   string
		repl     string
		expected string
	}{
		{
			name:     "align",
			anchor:   "        offset = _align_up(offset, align)",
			repl:     "        offset = _align_up(offset, 1)",
			expected: "array start not 64-byte aligned",
		},
		{
			name:     "count",
			anchor:   "if type(count) is not int or count < 0 or count > a.maximum:",
			repl:     "if type(count) is not int or count < 0:",
			expected: "over-count accepted",
		},
		{
			name:     "extent",
			anchor:   "used = _align_up(offset, _block.BLOCK_ALIGN)",
			repl:     "used = offset + 1",
			expected: "extent not 64-byte aligned",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			patch := func(files map[string][]byte) {
				pyBlockPatchOnce(t, files, u.Package+"/_schema/blockbuild.py", tc.anchor, tc.repl)
			}
			for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
				out, err := runProbe(t, pin, patch)
				if err == nil || !strings.Contains(out, tc.expected) {
					t.Fatalf("NEGATIVE CONTROL FAILED: %s did not fail on %q: %q %v", tc.name, tc.expected, out, err)
				}
			}
		})
	}
}

func hexDecode(line string) ([]byte, error) {
	return hex.DecodeString(line)
}

const pyBlockOpenProbe = `package blockdemo

import (
	"os"
	"testing"
	"unsafe"
)

func aligned(data []byte) unsafe.Pointer {
	raw := make([]byte, len(data)+64)
	base := uintptr(unsafe.Pointer(&raw[0]))
	off := (64 - base%64) % 64
	copy(raw[off:], data)
	return unsafe.Pointer(&raw[off])
}

func TestPythonBlockOpen(t *testing.T) {
	t.Run("RenderFrame", func(t *testing.T) {
		var storage RenderFrameBlockStorage
		if !storage.Create(TableBlockDefaultAllocator()) {
			t.Fatal("create")
		}
		defer storage.Destroy()
		var block RenderFrameBlock
		var refusal TableBlockRefusal
		counts := RenderFrameCounts{Cameras: 1, Ships: 3, Turrets: 2, Missiles: 1,
			DynamicProps: 2, StaticProps: 4, CosmeticProps: 1, Lasers: 3, Explosions: 2}
		if !RenderFrameBlockBegin(&block, &storage, counts, &refusal) {
			t.Fatalf("begin: %+v", refusal)
		}
		if err := os.WriteFile("go_RenderFrame.bin", unsafe.Slice((*byte)(block.Base), block.Bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		pythonBytes, err := os.ReadFile("py_RenderFrame.bin")
		if err != nil {
			t.Fatal(err)
		}
		var opened RenderFrameBlock
		if !RenderFrameBlockOpen(&opened, aligned(pythonBytes), int64(len(pythonBytes))) {
			t.Fatal("the Python-built block does not open")
		}
		if opened.Bytes != int64(len(pythonBytes)) ||
			opened.Projection.Ships.OffsetOf != block.Projection.Ships.OffsetOf ||
			opened.Projection.Ships.Count != 3 || opened.Projection.Ships.Stride != block.Projection.Ships.Stride ||
			opened.Projection.Explosions.OffsetOf != block.Projection.Explosions.OffsetOf {
			t.Fatalf("opened layout differs: %+v", opened.Projection)
		}
	})
	t.Run("PaddedFrame", func(t *testing.T) {
		var storage PaddedFrameBlockStorage
		if !storage.Create(TableBlockDefaultAllocator()) {
			t.Fatal("create")
		}
		defer storage.Destroy()
		var block PaddedFrameBlock
		var refusal TableBlockRefusal
		if !PaddedFrameBlockBegin(&block, &storage, PaddedFrameCounts{Rows: 3}, &refusal) {
			t.Fatalf("begin: %+v", refusal)
		}
		if err := os.WriteFile("go_PaddedFrame.bin", unsafe.Slice((*byte)(block.Base), block.Bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		pythonBytes, err := os.ReadFile("py_PaddedFrame.bin")
		if err != nil {
			t.Fatal(err)
		}
		var opened PaddedFrameBlock
		if !PaddedFrameBlockOpen(&opened, aligned(pythonBytes), int64(len(pythonBytes))) {
			t.Fatal("the Python-built block does not open")
		}
		if opened.Bytes != int64(len(pythonBytes)) || opened.Projection.Rows.OffsetOf != block.Projection.Rows.OffsetOf ||
			opened.Projection.Rows.Stride != block.Projection.Rows.Stride {
			t.Fatalf("opened layout differs: %+v", opened.Projection)
		}
	})
	t.Run("PaddedRow", func(t *testing.T) {
		var storage PaddedRowBlockStorage
		if !storage.Create(TableBlockDefaultAllocator()) {
			t.Fatal("create")
		}
		defer storage.Destroy()
		var block PaddedRowBlock
		if !PaddedRowBlockBegin(&block, &storage, PaddedRowCounts{}, nil) {
			t.Fatal("begin")
		}
		if err := os.WriteFile("go_PaddedRow.bin", unsafe.Slice((*byte)(block.Base), block.Bytes), 0o644); err != nil {
			t.Fatal(err)
		}
		pythonBytes, err := os.ReadFile("py_PaddedRow.bin")
		if err != nil {
			t.Fatal(err)
		}
		var opened PaddedRowBlock
		if !PaddedRowBlockOpen(&opened, aligned(pythonBytes), int64(len(pythonBytes))) {
			t.Fatal("the Python-built block does not open")
		}
		if opened.Bytes != int64(len(pythonBytes)) {
			t.Fatalf("opened extent %d, want %d", opened.Bytes, len(pythonBytes))
		}
	})
}
`
