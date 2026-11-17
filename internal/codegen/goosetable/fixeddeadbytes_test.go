package goosetable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4, §13.10, §22.6;
// docs/rulings.json fixed-dead-bytes-are-zero). The reference's dead-byte
// corpus stains one dead region at a time: an absent optional's payload, a
// counted array's slots past its count, a string(N)'s or bytes(N)'s bytes past
// its used length, and an inactive union arm. The generated load refuses the
// whole load for each, before a record lands, and the control — the same
// record with every dead byte zero — reads and writes back the reference's
// bytes. The verdict is the contract; the refusal's name is this leg's own.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose (it runs the generated reader over the fixed-form corpus)")
	corpus := fixedCorpus(t)
	u := fixedUnit(t, "test/tables/DeadBytes.schema")
	ns := goose.Namespace(u)
	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			data := gooseCorpusFile(t, corpus, vec.File)
			got := deadBytesProbe(t, u, ns, data)
			if !vec.Reads {
				if got.n != -1 {
					t.Fatalf("%s: the generated load answered n=%d; a dead byte that is not zero refuses the whole load", vec.File, got.n)
				}
				if got.after != got.before {
					t.Fatalf("%s: a refusal wrote the caller's destination\n was %s\n now %s", vec.File, got.before, got.after)
				}
				return
			}
			if got.n != 1 {
				t.Fatalf("%s: the control answered n=%d; it must read its one record", vec.File, got.n)
			}
			if want := hex.EncodeToString(data); got.written != len(data) || got.saved != want {
				t.Fatalf("%s: the control's write differs from the reference's image\n got %d %s\nwant %d %s",
					vec.File, got.written, got.saved, len(data), want)
			}
		})
	}
}

// deadBytesProbeResult is one generated program's answer for one corpus file.
type deadBytesProbeResult struct {
	n       int
	reason  string
	before  string
	after   string
	written int
	saved   string
}

// deadBytesProbe loads data through DeadRootFixedLoad into a destination whose
// record image is printed before and after, then writes a reading load back
// through DeadRootFixedSave.
func deadBytesProbe(t *testing.T, u *ir.Unit, ns string, data []byte) deadBytesProbeResult {
	t.Helper()
	main := fmt.Sprintf(`fn image(v: const %[1]s::DeadRoot&) {
    var img: u8[>..] = [];
    %[1]s::_DeadRootFixedWrite(img, v);
    hex(img);
}

fn main() {
    var data: u8[>..] = [];
    if !read_file("in.bin", data) { print("read-failed"); return; }
    var values: %[1]s::DeadRoot[1] = [%[1]s::DeadRoot { .. }; 1];
    values[0].ArrCount = 1;
    values[0].Arr[0] = 12345;
    values[0].TextLength = 1;
    values[0].Text[0] = 65;
    values[0].Pick.Type = 1;
    values[0].Pick.Narrow = 7;
    print("before");
    image(values[0]);
    var rep = schema_rt::FixedReport { .. };
    let n = %[1]s::DeadRootFixedLoad(values, data, rep);
    print("report ", n, " ", schema_rt::fixed_refusal_name(rep.reason));
    print("after");
    image(values[0]);
    if n >= 0 {
        var out: u8[>..] = [];
        let w = %[1]s::DeadRootFixedSave(values[0..n], out);
        print("saved ", w);
        hex(out);
    }
}
`, ns)
	out := runCustomFixedGoose(t, u, nil, main, map[string][]byte{"in.bin": data})
	return deadBytesParse(t, out)
}

func deadBytesParse(t *testing.T, out string) deadBytesProbeResult {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	next := func(i int) string {
		if i+1 >= len(lines) {
			t.Fatalf("the probe ended after %q:\n%s", lines[i], out)
		}
		return lines[i+1]
	}
	var r deadBytesProbeResult
	sawReport := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "report "):
			f := strings.Fields(l)
			if len(f) != 3 {
				t.Fatalf("an unreadable report line %q", l)
			}
			r.n, _ = strconv.Atoi(f[1])
			r.reason = f[2]
			sawReport = true
		case l == "before":
			r.before = next(i)
			i++
		case l == "after":
			r.after = next(i)
			i++
		case strings.HasPrefix(l, "saved "):
			f := strings.Fields(l)
			if len(f) != 2 {
				t.Fatalf("an unreadable saved line %q", l)
			}
			r.written, _ = strconv.Atoi(f[1])
			r.saved = next(i)
			i++
		}
	}
	if !sawReport {
		t.Fatalf("the probe printed no report:\n%s", out)
	}
	return r
}

// AN ABSENT OPTIONAL'S PAYLOAD IS DEAD WHATEVER ITS TYPE
// (docs/FIXED-FORM-ALGORITHM.md §4.5, §5.3; docs/SPEC-TABLES.md §3.4;
// docs/rulings.json fixed-dead-bytes-are-zero). Cfg's `extra ?Inner` is absent
// in the lawful v1_cfg and v2_cfg seeds, and its payload is a record: the seed
// loads, and the same file with any one byte of that payload set to 1 refuses
// the whole load. The verdict is the contract.
func TestGooseFixedAbsentRecordOptionalIsZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose (it runs the generated reader over the fixed-form seeds)")
	for _, c := range []struct{ schema, file string }{
		{"test/tables/V1.schema", "v1_cfg.bin"},
		{"test/tables/V2.schema", "v2_cfg.bin"},
	} {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			u := fixedUnit(t, c.schema)
			ns := goose.Namespace(u)
			seed, err := os.ReadFile(filepath.Join(fixedRoot, "testdata", "conformance", "tables", "wire", c.file))
			if err != nil {
				t.Fatal(err)
			}
			flag, size := absentExtraSpan(t, u, seed)
			files := map[string][]byte{"seed.bin": seed}
			names := []string{"seed.bin"}
			for k := int64(1); k < size; k++ {
				m := append([]byte(nil), seed...)
				m[flag+k] = 1
				name := fmt.Sprintf("m%d.bin", flag+k)
				files[name] = m
				names = append(names, name)
			}
			got := absentOptionalProbe(t, u, ns, names, files)
			if got["seed.bin"] != 1 {
				t.Fatalf("%s: the lawful seed answered n=%d; it must read its one record", c.file, got["seed.bin"])
			}
			for _, name := range names[1:] {
				if got[name] != -1 {
					t.Errorf("%s: byte %s of the absent `extra` payload set to 1 answered n=%d; a dead byte that is not zero refuses the whole load",
						c.file, strings.TrimSuffix(strings.TrimPrefix(name, "m"), ".bin"), got[name])
				}
			}
		})
	}
	t.Run("nested", func(t *testing.T) {
		t.Parallel()
		testNestedAbsentRecordOptional(t)
	})
}

// nestedAbsentSchema holds Holder, a record whose one field is an absent
// record optional over a record that bounds nothing, and reaches it through
// each way a record nests in another: a field, a fixed array's slots, a
// present optional's payload and a union's taken arm.
const nestedAbsentSchema = `package tblnest

fixed table Inner
{
    x float32
}

fixed table Holder
{
    extra ?Inner
}

union Pick
{
    narrow int32
    hold   Holder
}

fixed table ByField
{
    h Holder
}

fixed table ByArray
{
    hs [2]Holder
}

fixed table ByOptional
{
    ho ?Holder
}

fixed table ByArm
{
    pick Pick
}
`

// testNestedAbsentRecordOptional saves each root's lawful record through the
// generated writer, then loads it (accepted) and the same image with each byte
// of every absent `extra` payload set to 1 (each refused, verdict only).
func testNestedAbsentRecordOptional(t *testing.T) {
	u := parseCheckUnit(t, nestedAbsentSchema)
	ns := goose.Namespace(u)
	holder := fixedRootStruct(t, u, "Holder")
	extra := ir.TableFixedFieldBytes(holder.Fields[0])
	pick := fixedRootStruct(t, u, "ByArm").Fields[0].Type.Ref.(*ir.Union)
	arm := ir.TableFixedUnionTagBytes(pick)
	stride := ir.TableFixedElementBytes(fixedRootStruct(t, u, "ByArray").Fields[0])
	// each root: the Goose lines that make its lawful record, and the record
	// offsets of the present flags of its absent `extra` optionals
	roots := []struct {
		name  string
		setup string
		flags []int64
	}{
		{"ByField", "", []int64{0}},
		{"ByArray", "", []int64{0, stride}},
		{"ByOptional", "v[0].HoPresent = true;", []int64{ir.TableFixedPresentBytes}},
		{"ByArm", "v[0].Pick.Type = 2;", []int64{arm}},
	}
	var b strings.Builder
	b.WriteString("fn main() {\n")
	for _, r := range roots {
		fmt.Fprintf(&b, "    {\n        var v: %[1]s::%[2]s[1] = [%[1]s::%[2]s { .. }; 1];\n        %[3]s\n", ns, r.name, r.setup)
		fmt.Fprintf(&b, "        var out: u8[>..] = [];\n        let w = %s::%sFixedSave(v[0..1], out);\n", ns, r.name)
		fmt.Fprintf(&b, "        print(\"saved %s \", w);\n        hex(out);\n    }\n", r.name)
	}
	b.WriteString("}\n")
	out := runCustomFixedGoose(t, u, nil, b.String(), nil)
	lines := strings.Split(out, "\n")
	images := map[string][]byte{}
	for i, l := range lines {
		f := strings.Fields(l)
		if len(f) == 3 && f[0] == "saved" && i+1 < len(lines) {
			data, err := hex.DecodeString(strings.TrimSpace(lines[i+1]))
			if err != nil {
				t.Fatalf("an unreadable image after %q: %v", l, err)
			}
			images[f[1]] = data
		}
	}
	files := map[string][]byte{}
	var loads strings.Builder
	type mutant struct{ root, file string }
	var mutants []mutant
	for _, r := range roots {
		img, ok := images[r.name]
		if !ok || len(img) < 20 {
			t.Fatalf("the writer saved no image for %s:\n%s", r.name, out)
		}
		lb := int64(uint32(img[16]) | uint32(img[17])<<8 | uint32(img[18])<<16 | uint32(img[19])<<24)
		at := 20 + lb + 8
		seed := r.name + "-seed.bin"
		files[seed] = img
		fmt.Fprintf(&loads, "    probe_%s(%q);\n", r.name, seed)
		for _, fl := range r.flags {
			for k := range extra {
				if img[at+fl+k] != 0 {
					t.Fatalf("%s: the lawful record's absent extra is not zero (record byte %d is %d)", r.name, fl+k, img[at+fl+k])
				}
			}
			for k := int64(1); k < extra; k++ {
				m := append([]byte(nil), img...)
				m[at+fl+k] = 1
				name := fmt.Sprintf("%s-m%d.bin", r.name, fl+k)
				files[name] = m
				mutants = append(mutants, mutant{r.name, name})
				fmt.Fprintf(&loads, "    probe_%s(%q);\n", r.name, name)
			}
		}
	}
	var p strings.Builder
	for _, r := range roots {
		fmt.Fprintf(&p, "fn probe_%s(name: const u8[:]) {\n", r.name)
		fmt.Fprintf(&p, "    var data: u8[>..] = [];\n")
		fmt.Fprintf(&p, "    if !read_file(name, data) { print(\"read-failed\"); return; }\n")
		fmt.Fprintf(&p, "    var values: %[1]s::%[2]s[1] = [%[1]s::%[2]s { .. }; 1];\n", ns, r.name)
		fmt.Fprintf(&p, "    var rep = schema_rt::FixedReport { .. };\n")
		fmt.Fprintf(&p, "    let n = %s::%sFixedLoad(values, data, rep);\n", ns, r.name)
		fmt.Fprintf(&p, "    print(\"load \", name, \" \", n);\n}\n\n")
	}
	p.WriteString("fn main() {\n" + loads.String() + "}\n")
	out = runCustomFixedGoose(t, u, nil, p.String(), files)
	got := map[string]int{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "load" {
			n, err := strconv.Atoi(f[2])
			if err != nil {
				t.Fatalf("an unreadable load line %q", line)
			}
			got[f[1]] = n
		}
	}
	if len(got) != len(files) {
		t.Fatalf("the probe answered %d of %d loads:\n%s", len(got), len(files), out)
	}
	for _, r := range roots {
		if n := got[r.name+"-seed.bin"]; n != 1 {
			t.Errorf("%s: the lawful record answered n=%d; it must read its one record", r.name, n)
		}
	}
	for _, m := range mutants {
		if got[m.file] != -1 {
			t.Errorf("%s: %s, a byte of an absent nested `extra` payload set to 1, answered n=%d; a dead byte that is not zero refuses the whole load",
				m.root, m.file, got[m.file])
		}
	}
}

// absentExtraSpan is the file offset of Cfg.extra's present flag in the one
// record of data, and the field's whole size; the seed holds it absent with
// an all-zero payload.
func absentExtraSpan(t *testing.T, u *ir.Unit, data []byte) (int64, int64) {
	t.Helper()
	st := fixedRootStruct(t, u, "Cfg")
	lb := int64(uint32(data[16]) | uint32(data[17])<<8 | uint32(data[18])<<16 | uint32(data[19])<<24)
	at := 20 + lb + 8
	for _, f := range st.Fields {
		size := ir.TableFixedFieldBytes(f)
		if f.Name != "extra" {
			at += size
			continue
		}
		if !f.Type.Optional || at+size > int64(len(data)) {
			t.Fatalf("Cfg.extra is not an optional inside the seed's record")
		}
		for k := range size {
			if data[at+k] != 0 {
				t.Fatalf("the seed's Cfg.extra is not absent with a zero payload (byte %d is %d)", at+k, data[at+k])
			}
		}
		return at, size
	}
	t.Fatalf("Cfg has no field extra")
	return 0, 0
}

// absentOptionalProbe loads each named file through CfgFixedLoad into one
// record and answers each load's n.
func absentOptionalProbe(t *testing.T, u *ir.Unit, ns string, names []string, files map[string][]byte) map[string]int {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "fn probe(name: const u8[:]) {\n")
	fmt.Fprintf(&b, "    var data: u8[>..] = [];\n")
	fmt.Fprintf(&b, "    if !read_file(name, data) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %[1]s::Cfg[1] = [%[1]s::Cfg { .. }; 1];\n", ns)
	fmt.Fprintf(&b, "    var rep = schema_rt::FixedReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::CfgFixedLoad(values, data, rep);\n", ns)
	fmt.Fprintf(&b, "    print(\"load \", name, \" \", n);\n")
	fmt.Fprintf(&b, "}\n\nfn main() {\n")
	for _, n := range names {
		fmt.Fprintf(&b, "    probe(%q);\n", n)
	}
	fmt.Fprintf(&b, "}\n")
	out := runCustomFixedGoose(t, u, nil, b.String(), files)
	got := map[string]int{}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "load" {
			n, err := strconv.Atoi(f[2])
			if err != nil {
				t.Fatalf("an unreadable load line %q", line)
			}
			got[f[1]] = n
		}
	}
	if len(got) != len(names) {
		t.Fatalf("the probe answered %d of %d loads:\n%s", len(got), len(names), out)
	}
	return got
}
