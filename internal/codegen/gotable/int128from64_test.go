package gotable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// int128From64Values are the rows the mutation probe needs: a negative whose
// sign bit is the low lane's, math.MinInt64, and a value that is neither one
// lane's extreme nor small enough for a 32-bit widening to pass by accident.
// docs/SPEC-TABLES.md §3: an int128 is two 64-bit words, low then high, two's
// complement, so every negative int64 widened to int128 has Hi all ones.
var int128From64Values = []int64{-1, math.MinInt64, -4294967301}

// int128From64Fixture is one committed C++ wire and the narrow i64 wire that
// the same schema's later build must widen into its int128 field.
type int128From64Fixture struct {
	value  int64
	cpp    []byte
	narrow []byte
}

// int128From64Fixtures reads the three C++-written pins under
// testdata/wire/tables and builds the i64-kind wire a write from the narrowed
// schema emits: the byte-for-byte twin of the fixture's field, with kind 5 in
// place of kind 18 (docs/SPEC-TABLES.md §3's kind table).
func int128From64Fixtures(t *testing.T) []int128From64Fixture {
	t.Helper()
	names := []string{
		"int128from64_neg1.bin",
		"int128from64_min.bin",
		"int128from64_neg4294967301.bin",
	}
	ids := fieldIDByGoName(t)
	out := make([]int128From64Fixture, 0, len(names))
	for i, name := range names {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", name))
		if err != nil {
			t.Fatalf("the C++ writer's fixture %s: %v", name, err)
		}
		v := int128From64Values[i]
		narrow := []byte{1, 1, 5}
		narrow = binary.LittleEndian.AppendUint64(narrow, uint64(v))
		narrow = append(narrow, 0)
		narrow = binary.LittleEndian.AppendUint64(narrow, ids["flux"])
		narrow = binary.LittleEndian.AppendUint64(narrow, 1)
		out = append(out, int128From64Fixture{value: v, cpp: data, narrow: narrow})
	}
	return out
}

// fieldIDByGoName resolves the table-wire ids of the scalar fields the fixture
// exercises. The id is derived from the declaration name, never spelled here.
func fieldIDByGoName(t *testing.T) map[string]uint64 {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "scalars", "Scalars.schema"))
	if err != nil {
		t.Fatalf("the scalars schema: %v", err)
	}
	u := unitFrom(t, string(src))
	st := u.Tables["SimState"]
	if st == nil {
		t.Fatal("the scalars schema names no SimState table")
	}
	ids := make(map[string]uint64, len(st.Fields))
	for _, f := range st.Fields {
		ids[f.Name] = ir.TableFieldWireId(f)
	}
	return ids
}

// int128From64TestSource is the generated walk. It pins every generated group
// in one unit: the runtime widening itself, a narrow i64 wire widened into the
// int128 field, Save then Load, and the committed C++ wire both written and
// read. Its failure text names the sign extension so the reversed witness can
// tell the planted fault from a build failure.
func int128From64TestSource(fixtures []int128From64Fixture) string {
	var cases strings.Builder
	for _, f := range fixtures {
		fmt.Fprintf(&cases, "{v:%d, cpp:[]byte(%q), narrow:[]byte(%q)},\n", f.value, string(f.cpp), string(f.narrow))
	}
	return fmt.Sprintf(`package scalardemo

import (
	"bytes"
	"math"
	"testing"

	serialize "github.com/mas-bandwidth/serialize.go"
)

var int128Cases = []struct {
	v      int64
	cpp    []byte
	narrow []byte
}{
%s}

func wantLanes(v int64) (uint64, uint64) {
	if v < 0 {
		return ^uint64(0), uint64(v)
	}
	return 0, uint64(v)
}

func TestInt128From64Generated(t *testing.T) {
	t.Run("runtime-widening", func(t *testing.T) {
		for _, c := range int128Cases {
			hi, lo := wantLanes(c.v)
			x := serialize.Int128From64(c.v)
			if x.Hi != hi || x.Lo != lo {
				t.Fatalf("Int128From64(%%d): sign-extension lost, Hi=%%#x Lo=%%#x, want Hi=%%#x Lo=%%#x", c.v, x.Hi, x.Lo, hi, lo)
			}
			if x.Int64() != c.v {
				t.Fatalf("Int128From64(%%d).Int64() = %%d, want the same int64", c.v, x.Int64())
			}
		}
		for _, v := range []int64{0, 1, math.MaxInt64} {
			x := serialize.Int128From64(v)
			if x.Hi != 0 || x.Lo != uint64(v) {
				t.Fatalf("Int128From64(%%d): Hi=%%#x Lo=%%#x, want Hi=0 Lo=%%#x", v, x.Hi, x.Lo, uint64(v))
			}
		}
	})

	t.Run("narrow-wire-widening", func(t *testing.T) {
		for _, c := range int128Cases {
			var s SimState
			SimStateReset(&s)
			var r TableReport
			if !SimStateLoadTolerant(&s, c.narrow, &r) {
				t.Fatalf("the i64 wire for %%d was refused: %%+v", c.v, r)
			}
			if r.Widened != 1 {
				t.Fatalf("the i64 wire for %%d did not count one widen: %%+v", c.v, r)
			}
			hi, lo := wantLanes(c.v)
			if s.Flux.Hi != hi || s.Flux.Lo != lo {
				t.Fatalf("the generated read of the i64 wire for %%d lost the sign extension: Hi=%%#x Lo=%%#x, want Hi=%%#x Lo=%%#x", c.v, s.Flux.Hi, s.Flux.Lo, hi, lo)
			}
		}
	})

	t.Run("save-load", func(t *testing.T) {
		for _, c := range int128Cases {
			var s SimState
			SimStateReset(&s)
			s.Flux = serialize.Int128From64(c.v)
			wire := make([]byte, SimStateMeasure(&s))
			if n := SimStateSave(&s, wire); n != int64(len(wire)) {
				t.Fatalf("saving %%d: wrote %%d of %%d bytes", c.v, n, len(wire))
			}
			var out SimState
			var r TableReport
			if !SimStateLoadTolerant(&out, wire, &r) {
				t.Fatalf("loading %%d: refused: %%+v", c.v, r)
			}
			hi, lo := wantLanes(c.v)
			if out.Flux.Hi != hi || out.Flux.Lo != lo {
				t.Fatalf("the save/load round trip of %%d lost the sign extension: Hi=%%#x Lo=%%#x, want Hi=%%#x Lo=%%#x", c.v, out.Flux.Hi, out.Flux.Lo, hi, lo)
			}
		}
	})

	t.Run("cpp-wire", func(t *testing.T) {
		for _, c := range int128Cases {
			var s SimState
			SimStateReset(&s)
			s.Flux = serialize.Int128From64(c.v)
			wire := make([]byte, SimStateMeasure(&s))
			if n := SimStateSave(&s, wire); n != int64(len(wire)) {
				t.Fatalf("saving %%d for the C++ comparison: wrote %%d of %%d bytes", c.v, n, len(wire))
			}
			if !bytes.Equal(wire, c.cpp) {
				t.Fatalf("the Go wire for %%d is %% x, the C++ writer emitted %% x", c.v, wire, c.cpp)
			}
			var out SimState
			var r TableReport
			if !SimStateLoadTolerant(&out, c.cpp, &r) {
				t.Fatalf("loading the C++ wire for %%d: refused: %%+v", c.v, r)
			}
			hi, lo := wantLanes(c.v)
			if out.Flux.Hi != hi || out.Flux.Lo != lo {
				t.Fatalf("the C++ wire for %%d landed Hi=%%#x Lo=%%#x, want Hi=%%#x Lo=%%#x", c.v, out.Flux.Hi, out.Flux.Lo, hi, lo)
			}
		}
	})
}
`, cases.String())
}

// TestGoInt128From64SignExtends is the sign-extension gate the mutation probe
// found missing. It runs the generated unit against the pinned serialize.go
// runtime (the runtime the emitted text names), then runs the SAME unit
// against a copy of that runtime with Int128From64's sign extension textually
// removed and requires every generated group to go red. The internal/tabletext
// path is checked directly: it implements the widening over big.Int and never
// calls serialize.Int128From64, so it is not in that plant's reach.
func TestGoInt128From64SignExtends(t *testing.T) {
	t.Parallel()

	fixtures := int128From64Fixtures(t)
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "scalars", "Scalars.schema"))
	if err != nil {
		t.Fatalf("the scalars schema: %v", err)
	}
	u := unitFrom(t, string(src))
	testSource := int128From64TestSource(fixtures)

	// THE TEXT PATH (docs/SPEC-TABLES.md §16.2): a 128-bit integer is a
	// decimal integer, exact in both directions, and its storage is the two
	// lanes the wire uses. There is no serialize.go call on this path.
	for _, v := range int128From64Values {
		token := strconv.FormatInt(v, 10)
		raw, exact, saturated := tabletext.ParseWide(token, ir.TableKindI128, 0)
		if !exact || saturated {
			t.Fatalf("tabletext reading %q: exact=%v saturated=%v, want exact and no saturation", token, exact, saturated)
		}
		bytes := tabletext.WideBytes(raw, ir.TableKindI128)
		lo := binary.LittleEndian.Uint64(bytes[0:8])
		hi := binary.LittleEndian.Uint64(bytes[8:16])
		if hi != ^uint64(0) || lo != uint64(v) {
			t.Fatalf("tabletext reading %q: Hi=%#x Lo=%#x, want Hi all ones Lo=%#x", token, hi, lo, uint64(v))
		}
		if got := tabletext.FormatWide(raw, ir.TableKindI128, 0); got != token {
			t.Fatalf("tabletext writing %q printed %q", token, got)
		}
		if back := tabletext.WideFromBytes(bytes, ir.TableKindI128); back.String() != token {
			t.Fatalf("tabletext bytes for %q read back %s", token, back)
		}
	}
	for _, v := range []int64{0, 1, math.MaxInt64} {
		token := strconv.FormatInt(v, 10)
		raw, exact, saturated := tabletext.ParseWide(token, ir.TableKindI128, 0)
		if !exact || saturated {
			t.Fatalf("tabletext reading %q: exact=%v saturated=%v, want exact and no saturation", token, exact, saturated)
		}
		bytes := tabletext.WideBytes(raw, ir.TableKindI128)
		if hi := binary.LittleEndian.Uint64(bytes[8:16]); hi != 0 {
			t.Fatalf("tabletext reading %q: Hi=%#x, want 0", token, hi)
		}
	}

	genuine := serializeGoRuntime(t)
	if out, err := runInt128Unit(t, u, genuine, testSource); err != nil {
		t.Fatalf("the genuine runtime is not green: %v\n%s", err, out)
	} else {
		t.Logf("the genuine runtime passed every generated group:\n%s", out)
	}

	planted := plantInt128SignExtension(t, genuine)
	out, err := runInt128Unit(t, u, planted, testSource)
	if err == nil {
		t.Fatalf("the planted runtime (Int128From64 without sign extension) stayed green; the test does not catch the fault:\n%s", out)
	}
	if !strings.Contains(string(out), "sign-extension lost") && !strings.Contains(string(out), "sign extension") {
		t.Fatalf("the planted runtime failed for a reason other than the sign extension: %v\n%s", err, out)
	}
	t.Logf("the planted runtime is red, failing line kept:\n%s", out)
}

// serializeGoRuntime is the pinned runtime the generated units import. The
// card's own test command names this test, so it must RUN: a missing module
// cache entry fails by name instead of skipping.
func serializeGoRuntime(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(goModCache(t), "github.com", "mas-bandwidth", "serialize.go@v1.15.1")
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("the generated unit needs the pinned serialize.go runtime at %s: %v", dir, err)
	}
	return dir
}

// runInt128Unit generates the table unit into a throwaway module, writes the
// generated walk, and runs it. It returns the combined output so a call can
// keep a planted fault's failing line.
func runInt128Unit(t *testing.T, u *ir.Unit, runtime, testSource string) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// plantInt128SignExtension copies the pinned runtime into t.TempDir and drops
// the sign extension from Int128From64's TEXT, the mutation the coordinator's
// probe applied (a negative int64 then widens with Hi=0).
func plantInt128SignExtension(t *testing.T, genuine string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "serialize.go")
	copyTree(t, genuine, dir)
	path := filepath.Join(dir, "int128.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the runtime's int128.go: %v", err)
	}
	const extension = "\tif x < 0 {\n\t\thi = ^uint64(0)\n\t}\n"
	planted := strings.Replace(string(data), extension, "", 1)
	if planted == string(data) {
		t.Fatalf("the sign extension text was not found in %s; the plant did not land", path)
	}
	if err := os.WriteFile(path, []byte(planted), 0o600); err != nil {
		t.Fatalf("planting the fault in %s: %v", path, err)
	}
	return dir
}

// copyTree copies a runtime checkout into t.TempDir so the plant never edits
// the shared module cache. Every error is checked: a half-copied runtime would
// make the planted run fail for the wrong reason.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copying the runtime %s: %v", src, err)
	}
}
