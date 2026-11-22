package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// F32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE IS
// HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each row
// owes; every port that saves a compressed float runs it.
const f32EndsCorpusDir = "../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// F32EndsRow is one cases.tsv row.
type f32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

func f32EndsRows(t *testing.T) []f32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(f32EndsCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []f32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, f32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

// TestCompressedF32EndsCorpus pins each committed .bin to the reference engine
// and runs every row on it. ends.<form>.bin is the reference's encoding of
// ends.json; each past-*.<form>.bin is ends.<form>.bin with exactly one
// float's four bytes replaced by the float32 one step past that end. Before the
// range was held at float32 the reference refused its own ends.variable.bin
// and ends.fixed.bin on read, and ends.json on the JSON read: 0.1f widened is
// 0.10000000149, above a float64 max of 0.1.
func TestCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(f32EndsCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	file := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(f32EndsCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	save := func(form string, in *tabletext.Instance) ([]byte, error) {
		if form == "variable" {
			return tablewire.EncodeVariable(m, in)
		}
		return tablewire.EncodeFixed(m, in)
	}
	readJSON := func(root string, src []byte) (*tabletext.Instance, bool) {
		in := m.New(m.Lookup(root))
		var r tabletext.Report
		return in, m.Read(in, src, &r)
	}

	// The pins: ends.<form>.bin is the reference's save of ends.json, and each
	// past file differs from it in one float only, at that end's bits.
	for root, form := range formOf {
		in, ok := readJSON(root, file("ends.json"))
		if !ok {
			t.Fatalf("ends.json: the reference refuses it under %s", root)
		}
		ends := file("ends." + form + ".bin")
		if got, err := save(form, in); err != nil || !bytes.Equal(got, ends) {
			t.Fatalf("ends.%s.bin is not the reference's %s save of ends.json (%v):\n have %s\n want %s", form, form, err, hex.EncodeToString(ends), hex.EncodeToString(got))
		}
		for _, p := range []struct {
			stem     string
			from, to uint32
		}{{"past-max", 0x3dcccccd, 0x3dccccce}, {"past-min", 0xbe99999a, 0xbe99999b}} {
			from, to := le32(p.from), le32(p.to)
			at := bytes.Index(ends, from)
			if at < 0 {
				t.Fatalf("ends.%s.bin holds no %08x", form, p.from)
			}
			want := append(append(append([]byte{}, ends[:at]...), to...), ends[at+4:]...)
			if got := file(p.stem + "." + form + ".bin"); !bytes.Equal(got, want) {
				t.Errorf("%s.%s.bin is not ends.%s.bin with %08x at byte %d replaced by %08x", p.stem, form, form, p.from, at, p.to)
			}
		}
	}

	for _, row := range f32EndsRows(t) {
		t.Run(row.name, func(t *testing.T) {
			own := formOf[row.root]
			var in *tabletext.Instance
			accepted := false
			switch {
			case row.op == "read" && row.form == "json":
				in, accepted = readJSON(row.root, file(row.input+".json"))
			case row.op == "read":
				if formOf[row.root] != row.form {
					t.Fatalf("form %s under root %s", row.form, row.root)
				}
				in = m.New(m.Lookup(row.root))
				var r tabletext.Report
				ok, err := tablewire.Decode(m, in, file(row.input+"."+row.form+".bin"), &r)
				accepted = ok && err == nil
				if !accepted && !strings.Contains(r.Reason, "out_of_range") {
					t.Errorf("refused, but not out_of_range: %v %+v", err, r)
				}
			case row.op == "write":
				var ok bool
				if in, ok = readJSON(row.root, file(row.input+".json")); !ok {
					t.Fatalf("%s.json: the reference refuses it", row.input)
				}
				bits, err := strconv.ParseUint(row.bits, 16, 32)
				if err != nil {
					t.Fatal(err)
				}
				f32EndsSet(t, in, row.path, float64(math.Float32frombits(uint32(bits))))
				got, err := save(row.form, in)
				if row.expect == "refused" {
					if err == nil {
						t.Errorf("the %s save was not refused: %s", row.form, hex.EncodeToString(got))
					}
					return
				}
				if err != nil {
					t.Fatalf("the %s save was refused: %v", row.form, err)
				}
				if want := file(row.expect + "." + row.form + ".bin"); !bytes.Equal(got, want) {
					t.Errorf("the %s save:\n have %s\n want %s", row.form, hex.EncodeToString(got), hex.EncodeToString(want))
				}
				return
			default:
				t.Fatalf("op %q", row.op)
			}
			if row.expect == "refused" {
				if accepted {
					t.Errorf("the %s read was not refused", row.form)
				}
				return
			}
			if !accepted {
				t.Fatalf("the %s read refused the value at the float32 end", row.form)
			}
			got, err := save(own, in)
			if err != nil {
				t.Fatalf("saving again what was just read: %v", err)
			}
			if want := file(row.expect + "." + own + ".bin"); !bytes.Equal(got, want) {
				t.Errorf("the %s save of what was read:\n have %s\n want %s", own, hex.EncodeToString(got), hex.EncodeToString(want))
			}
		})
	}
}

// F32EndsSet sets a leaf (`r`) or an array element (`xs[0]`) of a loaded value.
func f32EndsSet(t *testing.T, in *tabletext.Instance, path string, v float64) {
	t.Helper()
	name, index, ok := strings.Cut(path, "[")
	if !ok {
		oracleField(t, in, name).Cell.F = v
		return
	}
	slot, err := strconv.Atoi(strings.TrimSuffix(index, "]"))
	fv := oracleField(t, in, name)
	if err != nil || slot < 0 || slot >= len(fv.Elems) {
		t.Fatalf("%s: slot %q of %d", path, index, len(fv.Elems))
	}
	fv.Elems[slot].F = v
}
