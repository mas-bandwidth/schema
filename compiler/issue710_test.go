package compiler

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestIssue710DirectoryList(t *testing.T) {
	u := unitFromSource(t, "package p\ntable Root { items []int32 }\n")
	c := New()
	expanded, flat := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(expanded, "items"), 0755); err != nil {
		t.Fatal(err)
	}
	for i, value := range []string{"3", "7", "11"} {
		if err := os.WriteFile(filepath.Join(expanded, "items", fmt.Sprintf("%02d.json", i)), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(flat, "Root.json"), []byte(`{"items":[3,7,11]}`), 0644); err != nil {
		t.Fatal(err)
	}
	want, _, r, err := c.Pack(u, "Root", flat)
	if err != nil || !r.Silent() {
		t.Fatalf("flat: %+v %v", r, err)
	}
	_, _, _, err = c.Pack(u, "Root", expanded)
	if err == nil || !strings.Contains(err.Error(), "VARIABLE-LENGTH") {
		t.Fatalf("directory list must refuse before reading elements: %v", err)
	}
	m := tabletext.NewModel(u)
	v := m.New(u.Tables["Root"])
	var decoded tabletext.Report
	if ok, err := tablewire.Decode(m, v, want, &decoded); !ok || err != nil || !decoded.Silent() || v.Fields[0].Count != 3 {
		t.Fatalf("JSON list round trip: %+v %v", decoded, err)
	}
}

func TestIssue710UnpackPaths(t *testing.T) {
	c := New()
	for _, key := range []string{"../escape", "..", "a/b", `a\b`, "C:drive", "a\x00b"} {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			u := unitFromSource(t, "package p\ntable Root { x int32 | json = \""+key+"\" }\n")
			m := tabletext.NewModel(u)
			wire, err := tablewire.Encode(m, m.New(u.Tables["Root"]))
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(t.TempDir(), "out")
			if _, err := c.Unpack(u, "Root", wire, dir); err == nil {
				t.Fatalf("accepted unsafe tree key %q", key)
			}
			if _, err := c.UnpackOneFile(u, "Root", wire, dir); err != nil {
				t.Fatalf("one-file key %q: %v", key, err)
			}
			if _, _, _, err := c.Pack(u, "Root", dir); err != nil {
				t.Fatal(err)
			}
		})
	}
	u := unitFromSource(t, "package p\ntable Root { x int32 }\n")
	m := tabletext.NewModel(u)
	wire, err := tablewire.Encode(m, m.New(u.Tables["Root"]))
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "out")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "original")
	if err := os.WriteFile(outside, []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "x.json")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if _, err := c.Unpack(u, "Root", wire, dir); err == nil {
		t.Fatal("followed outside symlink")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "unchanged" {
		t.Fatalf("outside changed: %q %v", got, err)
	}
}

// Small, explicit file-form fixtures test the recovery contract, with a sibling
// after the damaged field so resetting the value cannot hide a stopped parent.
func issue710Wire(body []byte, ids ...uint64) []byte {
	wire := append([]byte{ir.TableWireForm}, body...)
	wire = append(wire, 0)
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
}

func TestIssue710TableRecovery(t *testing.T) {
	src := `package p
 table Text { label string(32) = "untitled"; after int32 }
 table Element { value int32 = 7 }
 table Rows { items [..2]Element; after int32 }
 table Mapping { items map[uint32]Element; after int32 }
 `
	// The grammar terminates fields by newlines rather than semicolons.
	u := unitFromSource(t, strings.ReplaceAll(src, "; ", "\n"))
	m := tabletext.NewModel(u)
	fieldID := func(root string, i int) uint64 { return ir.TableFieldWireId(u.Tables[root].Fields[i]) }
	after := []byte{2, byte(ir.TableKindI32), 42, 0, 0, 0}
	var cases = []struct {
		name, root string
		wire       []byte
		check      func(*tabletext.Instance) bool
		cpp        string
	}{
		{"default", "Text", issue710Wire(append([]byte{1, 12, 2, 'x', 0}, after...), fieldID("Text", 0), fieldID("Text", 1)), func(v *tabletext.Instance) bool {
			return string(v.Fields[0].Cell.Str) == "untitled" && v.Fields[0].Count == 8
		}, `Text v; TableReport r; if (!TextLoad(v, wire, sizeof(wire), &r) || !r.malformed || v.after!=42 || v.label_length!=8 || strcmp(v.label,"untitled")!=0) return 1;`},
		{"array", "Rows", issue710Wire(append([]byte{1, 14, 11, 13, 1, 8, 3, byte(ir.TableKindI32), 99, 0, 0, 0, 0, 0}, after...), fieldID("Rows", 0), fieldID("Rows", 1), fieldID("Element", 0)), func(v *tabletext.Instance) bool {
			return v.Fields[0].Count == 1 && v.Fields[0].Elems[0].Tab.Fields[0].Cell.I == 7
		}, `Rows v; TableReport r; if (!RowsLoad(v, wire, sizeof(wire), &r) || !r.malformed || v.after!=42 || v.items_count!=1 || v.items[0].value!=7) return 2;`},
		{"map", "Mapping", issue710Wire(append([]byte{1, 14, 11, 13, 1, 8, 3, byte(ir.TableKindU32), 1, 0, 0, 0, 0, 0}, after...), fieldID("Mapping", 0), fieldID("Mapping", 1), ir.MapKeyWireId), func(v *tabletext.Instance) bool { return len(v.Fields[0].Entries) == 0 }, `MappingBuilder b; TableReport r; const bool ok=MappingLoadBuilder(b,wire,sizeof(wire),&r); const Mapping * v=b.GetRoot(); const bool pass=ok && r.malformed && v && v->after==42 && v->items.count==0; if(!pass) return 3;`},
	}
	c := New()
	files, err := c.Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var cpp strings.Builder
	cpp.WriteString("#include <cstring>\n#include \"ProbeTable.h\"\nusing namespace p;\nint main(){\n")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := m.New(u.Tables[tc.root])
			var r tabletext.Report
			ok, err := tablewire.Decode(m, v, tc.wire, &r)
			if !ok || err != nil || !r.Malformed || r.KindMismatch != 0 || v.Fields[1].Cell.I != 42 || !tc.check(v) {
				t.Fatalf("decode: ok=%v err=%v report=%+v value=%+v", ok, err, r, v.Fields)
			}
		})
		cpp.WriteString("{const uint8_t wire[]={")
		for _, b := range tc.wire {
			fmt.Fprintf(&cpp, "%d,", b)
		}
		cpp.WriteString("};\n" + tc.cpp + "\n}\n")
	}
	cpp.WriteString("return 0;}\n")
	issue710CompileRun(t, dir, "c++", ".cpp", cpp.String())
}

func issue710CompileRun(t *testing.T, dir, compiler, ext, source string) {
	t.Helper()
	if _, err := exec.LookPath(compiler); err != nil {
		t.Skipf("%s unavailable: %v", compiler, err)
	}
	path := filepath.Join(dir, "test"+ext)
	bin := filepath.Join(dir, "test")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-O1", "-g", "-Wall", "-Wextra", "-Werror"}
	if ext == ".cpp" {
		args = append(args, "-std=c++17")
	} else {
		args = append(args, "-std=c99")
	}
	if os.Getenv("SCHEMA_TEST_SANITIZE") == "1" {
		args = append(args, "-fsanitize=address,undefined", "-fno-sanitize-recover=all")
	}
	args = append(args, path, "-o", bin)
	if out, err := exec.Command(compiler, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
}

// Run the emitted helpers at the actual signed length limit. The single 2 GiB
// allocation is reused for both scans, freed before the next language runs,
// and omitted by go test -short. No packet or consumer process is involved.
func TestIssue710PacketStringBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("INT32_MAX string helper boundary requires a 2 GiB allocation")
	}
	u := unitFromSource(t, "package p\ntype Text { label string(8) }\n")
	c := New()
	for _, lang := range []string{"c", "cpp"} {
		t.Run(lang, func(t *testing.T) {
			files, err := c.Generate(u, lang, Options{})
			if err != nil {
				t.Fatal(err)
			}
			var source string
			for _, data := range files {
				if strings.Contains(string(data), "schema_utf8_valid") {
					source = string(data)
					break
				}
			}
			suffix, compiler, ext := "", "c++", ".cpp"
			if lang == "c" {
				suffix = "_"
				compiler = "cc"
				ext = ".c"
			}
			extract := func(name string) string {
				at := strings.Index(source, name+"(")
				if at < 0 {
					at = strings.Index(source, name+" (")
				}
				if at < 0 {
					t.Fatalf("missing %s", name)
				}
				begin := strings.LastIndex(source[:at], "\n") + 1
				open := at + strings.Index(source[at:], "{")
				depth := 1
				end := open + 1
				for ; end < len(source) && depth > 0; end++ {
					if source[end] == '{' {
						depth++
					}
					if source[end] == '}' {
						depth--
					}
				}
				if depth != 0 {
					t.Fatal("unclosed helper")
				}
				return source[begin:end] + "\n"
			}
			program := `#include <stdint.h>
#include <limits.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#define SCHEMA_UNUSED
#define SCHEMA_READ_INLINE inline
#define SCHEMA_C_READ_INLINE inline
typedef uint8_t serialize_uint8_t;
typedef uint32_t serialize_uint32_t;
typedef uint64_t serialize_uint64_t;
` + extract("schema_utf8_valid"+suffix) + extract("schema_interior_null"+suffix) + fmt.Sprintf(`
int main(void) {
    const int32_t n=INT32_MAX;
    uint8_t *p=(uint8_t *)malloc((size_t)n);
    if(!p) {fprintf(stderr,"INT32_MAX test allocation failed\n");return 10;}
    memset(p, 'a', (size_t)n);
    if(schema_interior_null%s(p,n)) return 11;
    p[n-1]=0;
    if(!schema_interior_null%s(p,n)) return 12;
    p[n-1]='a';
    p[n-2]=0xf0;
    if(schema_utf8_valid%s(p,n)) return 13;
    p[n-2]='a';
    p[n-4]=0xf0;p[n-3]=0x90;p[n-2]=0x80;p[n-1]=0x80;
    if(!schema_utf8_valid%s(p,n)) return 14;
    free(p);
    return 0;
}
`, suffix, suffix, suffix, suffix)
			issue710CompileRun(t, t.TempDir(), compiler, ext, program)
		})
	}
}

func TestIssue710ListRefusal(t *testing.T) {
	u := unitFromSource(t, "package p\ntable Lists { items []int32\n after int32 }\ntable Nested { child Lists\n after int32 }\n")
	m := tabletext.NewModel(u)
	ids := []uint64{ir.TableWireId("items"), ir.TableWireId("after"), ir.TableWireId("child")}
	// count 2^31, followed by a sibling that must never decode.
	body := []byte{1, 14, 6, byte(ir.TableKindI32), 0x80, 0x80, 0x80, 0x80, 8, 2, byte(ir.TableKindI32), 42, 0, 0, 0}
	nested := append([]byte{3, 13, byte(len(body) + 1)}, body...)
	nested = append(nested, 0, 2, byte(ir.TableKindI32), 42, 0, 0, 0)
	dir := t.TempDir()
	files, err := New().Generate(u, "cpp", Options{})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var cpp strings.Builder
	cpp.WriteString("#include \"ProbeTable.h\"\nusing namespace p;\nint main(){\n")
	for _, tc := range []struct {
		root string
		body []byte
	}{{"Lists", body}, {"Nested", nested}} {
		wire := issue710Wire(tc.body, ids...)
		v := m.New(u.Tables[tc.root])
		var r tabletext.Report
		ok, err := tablewire.Decode(m, v, wire, &r)
		_, countRefused := errors.AsType[*tablewire.CountRefusal](err)
		if ok || !countRefused || r.Malformed || r.Unknown != 0 || r.KindMismatch != 0 || v.Fields[1].Cell.I != 0 {
			t.Fatalf("%s refusal: ok=%v err=%v report=%+v", tc.root, ok, err, r)
		}
		// Count refusal is distinct from a pre-decode form refusal: public
		// tools must return it as an error, not report a successful empty read.
		if _, err := New().ReadReport(u, tc.root, wire); err == nil {
			t.Fatal("ReadReport hid the storage-cap refusal")
		}
		out := filepath.Join(t.TempDir(), "not-written")
		if _, err := New().Unpack(u, tc.root, wire, out); err == nil {
			t.Fatal("Unpack hid the storage-cap refusal")
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Fatalf("refused unpack touched the output directory: %v", err)
		}
		cpp.WriteString("{const uint8_t wire[]={")
		for _, b := range wire {
			fmt.Fprintf(&cpp, "%d,", b)
		}
		fmt.Fprintf(&cpp, "}; %sBuilder b; TableReport r; if(%sLoadBuilder(b,wire,sizeof(wire),&r) || r.malformed || r.unknown || r.kind_mismatch || b.GetRoot()->after!=0) return 1; TableRefuseReason reason = count_over_length; if(%sLoadMeasure(wire,sizeof(wire),NULL,&reason)!=-1 || reason!=count_over_extent_cap) return 2; }\n", tc.root, tc.root, tc.root)
	}
	cpp.WriteString("return 0;}\n")
	issue710CompileRun(t, dir, "c++", ".cpp", cpp.String())
}
