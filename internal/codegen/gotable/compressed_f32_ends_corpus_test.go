package gotable

// A COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3),
// through the GENERATED Go: the cross-port corpus
// testdata/conformance/feature-witnesses/compressed-f32-ends, every cases.tsv
// row run on the generated unit in its own runtime. The value at each float32
// end (0.1f, -0.3f) reads, saves and reads back to the same bytes in the
// variable, fixed and JSON forms; one float32 step past either end is refused
// by every reader and every writer. Beside the rows, each write row also asks
// the JSON writer for the same verdict, and the cook writer for acceptance at
// the ends.

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

const f32EndsCorpus = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

const f32EndsProbe = `package f32ends

import ("bytes"; "encoding/hex"; "math"; "os"; "strconv"; "strings"; "testing")

func f32File(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// f32Value is one loaded root: V (the variable form) or F (the fixed form).
type f32Value struct {
	v *V
	f *F
}

func f32ReadJSON(t *testing.T, root string, src []byte) (f32Value, bool) {
	t.Helper()
	var report TableReport
	if root == "V" {
		b := new(VBuilder)
		if !b.Init() {
			t.Fatal("init")
		}
		ok := VFromJSON(b, src, &report) && report.Err() == nil
		return f32Value{v: b.GetRoot()}, ok
	}
	f := new(F)
	ok := FFromJSON(f, src, &report) && report.Err() == nil
	return f32Value{f: f}, ok
}

func f32ReadWire(root string, wire []byte) (f32Value, error) {
	if root == "V" {
		v, err := VReadStrict(wire)
		return f32Value{v: v}, err
	}
	f, err := FReadStrict(wire)
	return f32Value{f: f}, err
}

// f32Save is the root's own form: V the variable form, F the fixed form; nil
// is the writer's refusal.
func f32Save(x f32Value) []byte {
	if x.v != nil {
		n := VMeasure(x.v)
		if n < 0 {
			return nil
		}
		buf := make([]byte, n)
		if VSave(x.v, buf) != n {
			return nil
		}
		return buf
	}
	n := FMeasure(x.f)
	if n < 0 {
		return nil
	}
	buf := make([]byte, n)
	if FSave(x.f, buf) != n {
		return nil
	}
	return buf
}

// f32Cook and f32ToJSON are the other writers' verdicts on the same value.
func f32Cook(x f32Value) bool {
	if x.v != nil {
		n := VCookMeasure(x.v)
		return n >= 0 && VCookFrom(x.v, make([]byte, n), TableByteOrderLittle)
	}
	n := FCookMeasure(x.f)
	return n >= 0 && FCookFrom(x.f, make([]byte, n), TableByteOrderLittle)
}

func f32ToJSON(x f32Value) bool {
	if x.v != nil {
		n := VToJSONMeasure(x.v)
		return n >= 0 && VToJSON(x.v, make([]byte, n)) == n
	}
	n := FToJSONMeasure(x.f)
	return n >= 0 && FToJSON(x.f, make([]byte, n)) == n
}

func f32Set(t *testing.T, x f32Value, path string, v float32) {
	t.Helper()
	var r *float32
	var xs []float32
	if x.v != nil {
		r, xs = &x.v.R, x.v.Xs[:]
	} else {
		r, xs = &x.f.R, x.f.Xs[:]
	}
	switch path {
	case "r":
		*r = v
	case "xs[0]":
		xs[0] = v
	default:
		t.Fatalf("path %q", path)
	}
}

func TestCompressedF32EndsCorpus(t *testing.T) {
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	rows := 0
	for _, line := range strings.Split(string(f32File(t, "cases.tsv")), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv: %q has %d fields, want 8", line, len(f))
		}
		rows++
		name, op, form, root, input, path, bits, expect := f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]
		t.Run(name, func(t *testing.T) {
			own := formOf[root]
			var x f32Value
			accepted := false
			switch {
			case op == "read" && form == "json":
				x, accepted = f32ReadJSON(t, root, f32File(t, input+".json"))
			case op == "read":
				if own != form {
					t.Fatalf("form %s under root %s", form, root)
				}
				var err error
				x, err = f32ReadWire(root, f32File(t, input+"."+form+".bin"))
				// refusal-verdict-only (docs/rulings.json): the verdict is checked, never the reason.
				accepted = err == nil
			case op == "write":
				var ok bool
				if x, ok = f32ReadJSON(t, root, f32File(t, input+".json")); !ok {
					t.Fatalf("%s.json: the generated reader refuses it", input)
				}
				b, err := strconv.ParseUint(bits, 16, 32)
				if err != nil {
					t.Fatal(err)
				}
				f32Set(t, x, path, math.Float32frombits(uint32(b)))
				if form != own {
					t.Fatalf("form %s under root %s", form, root)
				}
				got := f32Save(x)
				cook, text := f32Cook(x), f32ToJSON(x)
				if expect == "refused" {
					if got != nil {
						t.Errorf("the %s save was not refused: %s", form, hex.EncodeToString(got))
					}
					// The cook writer holds no range test (only finiteness), so it is
					// asked here only for acceptance at the ends.
					if text {
						t.Errorf("past the end: the JSON writer accepted it")
					}
					return
				}
				if got == nil {
					t.Fatalf("the %s save was refused", form)
				}
				if !cook || !text {
					t.Errorf("at the end: the cook writer accepted %v, the JSON writer accepted %v; both owe acceptance", cook, text)
				}
				if want := f32File(t, expect+"."+form+".bin"); !bytes.Equal(got, want) {
					t.Errorf("the %s save:\n have %s\n want %s", form, hex.EncodeToString(got), hex.EncodeToString(want))
				}
				return
			default:
				t.Fatalf("op %q", op)
			}
			if expect == "refused" {
				if accepted {
					t.Errorf("the %s read was not refused", form)
				}
				return
			}
			if !accepted {
				t.Fatalf("the %s read refused the value at the float32 end", form)
			}
			got := f32Save(x)
			if got == nil {
				t.Fatal("saving again what was just read was refused")
			}
			if want := f32File(t, expect+"."+own+".bin"); !bytes.Equal(got, want) {
				t.Errorf("the %s save of what was read:\n have %s\n want %s", own, hex.EncodeToString(got), hex.EncodeToString(want))
			}
		})
	}
	if rows == 0 {
		t.Fatal("cases.tsv holds no rows")
	}
}
`

// TestCompressedF32EndsCorpusGo runs every cases.tsv row through the generated
// Go: the corpus files sit beside the generated unit, and the probe reads them
// from its own directory.
func TestCompressedF32EndsCorpusGo(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join(f32EndsCorpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f32EndsCorpus)
	if err != nil {
		t.Fatal(err)
	}
	corpus := map[string][]byte{}
	for _, e := range entries {
		if e.Name() == "Probe.schema" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(f32EndsCorpus, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		corpus[e.Name()] = b
	}
	out, err := runGeneratedEdited(t, string(schema), f32EndsProbe, func(files map[string][]byte) {
		maps.Copy(files, corpus)
	}, "-v")
	if err != nil {
		t.Fatalf("generated runtime: %v\n%s", err, out)
	}
}
