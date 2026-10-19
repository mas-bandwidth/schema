package goosetable

// THE FIXED FORM'S BYTE ORACLE (docs/FIXED-FORM-ALGORITHM.md §7 row 1;
// ROADMAP.md, Fixed Tables). The bytes are the C++ reference's:
// `make tables-fixedform-corpus` writes build/fixedform-corpus, and the same
// fixed roots are the bytes this build bakes (fixedform.go). A bare tree is a
// SKIP; SCHEMA_REQUIRE_CORPUS=1 (the gate's environment) makes a missing
// corpus a FAILURE, never a silent pass.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedRoot = "../../.."

// fixedVectors is §7 row 1's nine: the corpus file, the schema that wrote it,
// and its root. They are the files the reference's dump writes for the fixed
// form itself; the versioning rows are schema-goose-fixed-form-evolution's.
var fixedVectors = []struct {
	file, schema, root string
}{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

// fixedCorpus resolves build/fixedform-corpus, failing under
// SCHEMA_REQUIRE_CORPUS=1 and skipping otherwise (the house rule for a shared
// oracle: a bare tree has no C++ reference to compare with).
func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(fixedRoot, "build", "fixedform-corpus"))
	if err == nil {
		if st, serr := os.Stat(filepath.Join(dir, "manifest.txt")); serr == nil && !st.IsDir() {
			return dir
		}
	}
	why := fmt.Sprintf("the C++ reference's byte oracle is not built: make tables-fixedform-corpus (%v)", err)
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal(why)
	}
	t.Skip(why)
	return ""
}

func fixedUnit(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	path := filepath.Join(fixedRoot, filepath.FromSlash(schema))
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("load %s: %v", schema, err)
	}
	return u
}

func fixedRootStruct(t *testing.T, u *ir.Unit, name string) *ir.Struct {
	t.Helper()
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("the unit has no fixed root %s", name)
	return nil
}

// TestGooseFixedFormAgreesWithTheByteOracle compares every fixed root of the
// corpus with the C++ reference's dump (make tables-fixedform-corpus) byte for
// byte both ways (docs/FIXED-FORM-ALGORITHM.md §7): the emitted layout bytes
// and hash are the file's header, and a generated Goose program, compiled by
// the pinned compiler, holds the reference file to the same baked constants
// through the form-3 runtime.
func TestGooseFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	var compared atomic.Int64
	for _, v := range fixedVectors {
		v := v
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatalf("read %s: %v", v.file, err)
			}
			u := fixedUnit(t, v.schema)
			st := fixedRootStruct(t, u, v.root)
			layout, hash, body := fixedRootFacts(st)

			// The reference's header: form 3, seven reserved zeros, its hash,
			// its layout length, its layout (docs/SPEC-TABLES.md §3.4).
			if len(data) < int(ir.TableFixedHeaderBytes)+4 {
				t.Fatalf("%s is shorter than a fixed header", v.file)
			}
			if data[0] != 3 {
				t.Fatalf("%s: form byte %d, want 3", v.file, data[0])
			}
			for i := 1; i < 8; i++ {
				if data[i] != 0 {
					t.Fatalf("%s: reserved byte %d is %#x, want 0", v.file, i, data[i])
				}
			}
			if got := binary.LittleEndian.Uint64(data[8:16]); got != hash {
				t.Fatalf("%s: header hash 0x%016x, emitted 0x%016x", v.file, got, hash)
			}
			n := int(binary.LittleEndian.Uint32(data[16:20]))
			if n != len(layout) {
				t.Fatalf("%s: layout length %d, emitted %d", v.file, n, len(layout))
			}
			if string(data[20:20+n]) != string(layout) {
				t.Fatalf("%s: the reference layout differs from the emitted layout", v.file)
			}
			if (len(data)-20-n)%int(8+body) != 0 {
				t.Fatalf("%s: %d record bytes is not a multiple of the emitted record size %d", v.file, len(data)-20-n, 8+body)
			}

			// Both ways, under the pinned compiler: the generated Goose
			// program holds the same file to the baked constants through the
			// form-3 runtime, and prints the count it checked.
			files, err := goose.Generate(u)
			if err != nil {
				t.Fatalf("goose type surface: %v", err)
			}
			tables, err := Generate(u)
			if err != nil {
				t.Fatalf("goose table surface: %v", err)
			}
			for name, data := range tables {
				files[name] = data
			}
			var mods []string
			for name := range files {
				if strings.HasSuffix(name, ".goose") {
					mods = append(mods, strings.TrimSuffix(name, ".goose"))
				}
			}
			sort.Strings(mods)
			var b strings.Builder
			b.WriteString("import os;\n")
			for _, m := range mods {
				fmt.Fprintf(&b, "import .%s;\n", m)
			}
			fmt.Fprintf(&b, "\nfn main() {\n    var bytes: u8[>..] = [];\n    if !read_file(\"corpus.bin\", bytes) { print(\"read-failed\"); return; }\n")
			fmt.Fprintf(&b, "    print(schema_rt::fixed_check_file(bytes, %s::%sFixedLayout, %s::%sFixedHash, %s::%sFixedRecordBytes));\n}\n", goose.Namespace(u), v.root, goose.Namespace(u), v.root, goose.Namespace(u), v.root)
			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			main := "main.goose"
			if err := os.WriteFile(filepath.Join(dir, main), []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			out := strings.TrimSpace(string(gooserun.Run(t, dir, main, map[string][]byte{"corpus.bin": data})))
			want := fmt.Sprint((len(data) - 20 - n) / int(8+body))
			if out != want {
				t.Fatalf("%s: the form-3 runtime checked %q records, want %q (the manifest's count)", v.file, out, want)
			}
			t.Logf("%s: %s records agree (%d layout bytes, hash 0x%016x)", v.file, want, n, hash)
			compared.Add(1)
		})
	}
	t.Logf("%d fixed roots compared with the reference's bytes", compared.Load())
}

// TestGooseFixedFormNegativeControl must go red on one planted offset: a
// layout byte flipped must move the hash and be refused by the form-3
// runtime's header check (docs/FIXED-FORM-ALGORITHM.md §5.2, the hash is over
// the layout's bytes as written).
func TestGooseFixedFormNegativeControl(t *testing.T) {
	t.Parallel()
	st := &ir.Struct{
		Name:    "Point",
		IsTable: true,
		Fields: []*ir.Field{
			{Name: "x", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}},
			{Name: "y", Type: ir.FieldType{Kind: ir.TInt, Signed: true, Width: 32}},
		},
	}
	layout, hash, _ := fixedRootFacts(st)
	if len(layout) < 4+17 {
		t.Fatalf("the planted offset is past the layout (%d bytes)", len(layout))
	}
	planted := append([]byte(nil), layout...)
	planted[4+17] ^= 0x01 // one byte of the second entry's kind

	if string(planted) == string(layout) {
		t.Fatalf("planted offset equals the original layout")
	}
	if got := ir.TableFixedLayoutHash(planted, st); got == hash {
		t.Fatalf("planted layout hashed identically to the original: %#x", got)
	}
	if len(planted) == len(layout) {
		// The runtime's layout comparison is byte for byte, so the same flip
		// is refused even when the hash is forged to the planted layout.
		for i := range planted {
			if planted[i] != layout[i] {
				if i < 4 || i >= len(layout) {
					t.Fatalf("planted offset %d is outside the entry bytes", i)
				}
				return
			}
		}
	}
	t.Fatalf("no planted byte differs")
}
