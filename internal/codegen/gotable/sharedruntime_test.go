package gotable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// sharedRuntimeDir is the checked-in shared package, relative to this package.
const sharedRuntimeDir = "../../../tables/goruntime"

// TestGoSharedRuntimePackageIsCurrent pins "one source" (docs/SPEC-TABLES.md
// §8.4): tables/goruntime is exactly what the emitter derives, so the package
// and the inline copy cannot drift. SCHEMA_WRITE_GORUNTIME=1 rewrites it (make
// go-runtime).
func TestGoSharedRuntimePackageIsCurrent(t *testing.T) {
	t.Parallel()
	files, err := SharedRuntimeFiles()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		path := filepath.Join(sharedRuntimeDir, name)
		if os.Getenv("SCHEMA_WRITE_GORUNTIME") == "1" {
			if err := os.MkdirAll(sharedRuntimeDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (regenerate with make go-runtime)", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is not what the emitter derives (regenerate with make go-runtime)", path)
		}
	}
}

// sharedProgram saves, loads and damages a fixed unit and a variable unit, and
// prints one OUT line per observation: the wire bytes and the report. The two
// modes must print the same lines.
const sharedFixedSchema = `package probe
enum Mode { Fast, Slow }
union Value {
 tally int32 | min = 0, max = 100
 label string(8)
 ping
}
fixed table Root {
 title string(12) = "default"
 mode Mode
 value Value
 scores [..3]int32
 ratio float64 = 0.5
}
`

const sharedFixedTest = `package probe
import ("fmt";"testing";"encoding/hex")
func TestShared(t *testing.T) {
 var v Root; RootReset(&v)
 v.TitleLength = 3; copy(v.Title[:], "abc")
 v.Mode = ModeSlow
 v.ScoresCount = 2; v.Scores[0] = 7; v.Scores[1] = -9
 v.Ratio = 1.25
 wire := make([]byte, RootMeasure(&v))
 if RootSave(&v, wire) != int64(len(wire)) { t.Fatal("save") }
 fmt.Printf("OUT wire %s\n", hex.EncodeToString(wire))
 var got Root; var r TableReport
 ok := RootLoad(&got, wire, &r)
 fmt.Printf("OUT load %v %+v %v %d %v\n", ok, r, got.Mode, got.ScoresCount, got.Ratio)
 for n := 0; n < len(wire); n++ {
  var g Root; var rr TableReport
  ok := RootLoad(&g, wire[:n], &rr)
  fmt.Printf("OUT cut %d %v %+v\n", n, ok, rr)
 }
 for i := 0; i < len(wire); i++ {
  b := append([]byte(nil), wire...); b[i] ^= 0xff
  var g Root; var rr TableReport
  ok := RootLoad(&g, b, &rr)
  fmt.Printf("OUT flip %d %v %+v\n", i, ok, rr)
 }
 fmt.Printf("OUT refuse %v %v\n", TableRefuseTruncated.Error(), TableRefuseWireDamaged)
 fmt.Printf("OUT announce %d\n", len(tableAnnouncement))
}
`

const sharedVariableSchema = `package probe
fixed table Child { n int32 = 7 }
table Root {
 numbers []int32
 children []Child
 head *Child
 label string(8)
}
`

const sharedVariableTest = `package probe
import ("fmt";"testing";"encoding/hex";"unsafe")
func TestShared(t *testing.T) {
 var b RootBuilder
 if !b.Init() { t.Fatal("init") }
 defer b.Shutdown()
 v := b.GetRoot()
 for i := 0; i < 40; i++ { *RootNumbersAdd(&b.Main, &v.Numbers) = int32(i * 3) }
 RootChildrenAdd(&b.Main, &v.Children).N = 9
 ChildEmplace(&b.Main, &v.Head).N = 11
 n := RootMeasure(v, &b.Arena)
 wire := make([]byte, n)
 if RootSave(v, wire, &b.Arena) != n { t.Fatal("save") }
 fmt.Printf("OUT wire %s\n", hex.EncodeToString(wire))
 size := RootLoadMeasure(wire)
 raw := make([]byte, size+16)
 off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
 var r TableReport
 got := RootLoad(raw[off:off+uintptr(size)], wire, &r)
 fmt.Printf("OUT load %v %+v %d\n", got != nil, r, size)
 for c := 0; c < len(wire); c += 3 {
  rr := TableReport{}
  s := RootLoadMeasure(wire[:c])
  fmt.Printf("OUT cut %d %d %+v\n", c, s, rr)
 }
}
`

// sharedRun generates the unit in one mode, compiles it with its tests and
// returns the OUT lines. The shared mode imports tables/goruntime from this
// repository through a replace.
func sharedRun(t *testing.T, u *ir.Unit, testSource string, rt Runtime, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateRuntime(u, nil, rt)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	serialize := slowtest.SerializeGo(t, "../../..")
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\nreplace github.com/mas-bandwidth/schema/v2 => %q\n", serialize, root))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// not behind slowtest.Gate: the card's gate names this test and requires it
	// to RUN, and it compiles two small units, as runGenerated does.
	cmd := exec.Command("go", "test", "-count=1", "-v", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func outLines(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "OUT ") {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

var constOverflow = regexp.MustCompile(`overflows|Version`)

// TestGoSharedRuntimeIsOptionalAndEquivalent: the shared runtime is an option,
// standalone is untouched, and the same unit produces identical wires and
// reports either way (docs/SPEC-TABLES.md §8.4).
func TestGoSharedRuntimeIsOptionalAndEquivalent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, schema, test string }{
		{"fixed", sharedFixedSchema, sharedFixedTest},
		{"variable", sharedVariableSchema, sharedVariableTest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := unitFrom(t, tc.schema)

			// standalone is the default, byte for byte
			base, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			alone, err := GenerateRuntime(u, nil, RuntimeStandalone)
			if err != nil {
				t.Fatal(err)
			}
			if len(base) != len(alone) {
				t.Fatalf("standalone emits %d files, default %d", len(alone), len(base))
			}
			for name, want := range base {
				if !bytes.Equal(alone[name], want) {
					t.Fatalf("standalone %s differs from the default output", name)
				}
			}
			if r, err := ParseRuntime(""); err != nil || r != RuntimeStandalone {
				t.Fatalf("an absent option is standalone, got %v %v", r, err)
			}
			if _, err := ParseRuntime("bogus"); err == nil || !strings.Contains(err.Error(), "bogus") {
				t.Fatalf("an unknown runtime is refused by name: %v", err)
			}

			// shared drops the machinery and imports the package
			shared, err := GenerateRuntime(u, nil, RuntimeShared)
			if err != nil {
				t.Fatal(err)
			}
			home := runtimeHome(u) + "Table.go"
			if !bytes.Contains(shared[home], []byte(SharedRuntimeImport)) || bytes.Contains(shared[home], []byte("type TableReport struct")) {
				t.Fatalf("%s does not import the shared runtime in place of a copy", home)
			}
			if len(shared[home]) >= len(alone[home]) {
				t.Fatalf("shared %s is %d bytes, standalone %d", home, len(shared[home]), len(alone[home]))
			}

			// the same unit, both ways: identical wires and reports
			want, err := sharedRun(t, u, tc.test, RuntimeStandalone, nil)
			if err != nil {
				t.Fatalf("standalone: %v\n%s", err, want)
			}
			got, err := sharedRun(t, u, tc.test, RuntimeShared, nil)
			if err != nil {
				t.Fatalf("shared: %v\n%s", err, got)
			}
			if outLines(want) == "" {
				t.Fatalf("the program printed nothing:\n%s", want)
			}
			if outLines(want) != outLines(got) {
				wl, gl := strings.Split(outLines(want), "\n"), strings.Split(outLines(got), "\n")
				for i := range wl {
					if i >= len(gl) || wl[i] != gl[i] {
						t.Fatalf("standalone and shared disagree at line %d\nstandalone: %s\nshared:     %s", i, wl[i], gl[min(i, len(gl)-1)])
					}
				}
				t.Fatalf("standalone and shared disagree: %d lines against %d", len(wl), len(gl))
			}

			// a unit built against another runtime does not compile, whichever
			// way the two differ
			for _, edit := range []struct{ name, from, to string }{
				{"runtime newer", `goruntime\.Version - 0x[0-9a-f]{16}`, "goruntime.Version - 0xffffffffffffffff"},
				{"runtime older", `0x[0-9a-f]{16} - goruntime\.Version`, "0x0000000000000000 - goruntime.Version"},
			} {
				out, err := sharedRun(t, u, tc.test, RuntimeShared, func(files map[string][]byte) {
					re := regexp.MustCompile(edit.from)
					if !re.Match(files[home]) {
						t.Fatalf("%s: the unit asserts no version", home)
					}
					files[home] = re.ReplaceAll(files[home], []byte(edit.to))
				})
				if err == nil || !constOverflow.MatchString(out) {
					t.Fatalf("%s: a version mismatch must be a compile error, got err=%v\n%s", edit.name, err, out)
				}
			}
		})
	}
}

// TestGoSharedRuntimeCoversTheCorpora: every single-file table schema of the
// repository's corpora generates with runtime=shared (a declaration the unit
// spells differently from the package is an error, never a silent second
// copy), its standalone output is the default output byte for byte, and the
// shared home file is smaller than the standalone one. The count guard keeps
// an emptied glob from passing.
func TestGoSharedRuntimeCoversTheCorpora(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("../../../tables/*/*.schema")
	if err != nil {
		t.Fatal(err)
	}
	more, err := filepath.Glob("../../../tables/pack/*/*.schema")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range append(paths, more...) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		u, err := rtProbeUnit(string(src))
		if err != nil {
			continue // it needs a sibling file
		}
		base, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(base) == 0 {
			continue
		}
		alone, err := GenerateRuntime(u, nil, RuntimeStandalone)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for name, want := range base {
			if !bytes.Equal(alone[name], want) {
				t.Fatalf("%s: standalone %s differs from the default output", path, name)
			}
		}
		shared, err := GenerateRuntime(u, nil, RuntimeShared)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		home := runtimeHome(u) + "Table.go"
		if len(shared[home]) >= len(alone[home]) {
			t.Fatalf("%s: shared %s is %d bytes, standalone %d", path, home, len(shared[home]), len(alone[home]))
		}
		checked++
	}
	if checked < 20 {
		t.Fatalf("only %d corpus units were checked", checked)
	}
}
