package goosetable

// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §3.4,
// §21.2, §22.6; docs/rulings.json fixed-deprecated-field-is-carried): the
// reader lands the stored value and checks it as it checks any field of its
// kind, and the writer stores what the value holds and refuses what any field
// of its kind would refuse. The declared default applies only where the record
// does not supply the field. The shared vectors (fixedvector.DeprecatedField)
// hold a lawful value that is not the field's declared default: each file
// reads, lands the stored values the reference landed, and saves back byte for
// byte. A deprecated ENUM VARIANT (§2.15) is a different rule and is not here.

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated reader and writer over the corpus)")
	t.Parallel()
	corpus := fixedCorpus(t)
	u := fixedUnit(t, "test/tables/FD1.schema")
	for _, vec := range fixedvector.DeprecatedField {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused {
				t.Fatalf("%s: the reference landed no record, so there is no stored value to carry", vec.File)
			}
			var print []string
			for _, path := range want.Paths {
				expr, ok := deprecatedExpr(path)
				if !ok {
					t.Fatalf("%s: the manifest path %s is not record 0's", vec.File, path)
				}
				print = append(print, expr)
			}
			data := gooseCorpusFile(t, corpus, vec.File)
			got := gooseVersionProbe(t, gooseVersionCase{reader: u, root: vec.Root,
				capacity: 1, data: data, print: print})
			got.reads(t, 1)
			if len(got.printed) != len(want.Paths) {
				t.Fatalf("%s: printed %v, want %d stored values", vec.File, got.printed, len(want.Paths))
			}
			for k, path := range want.Paths {
				if got.printed[k] != deprecatedWant(path, want.Values[path]) {
					t.Errorf("%s: %s landed %s, want the stored %d", vec.File, path, got.printed[k], want.Values[path])
				}
			}

			dir := t.TempDir()
			main := fixedProgram(t, u, vec.Root, dir, []string{"corpus.bin"}, 1)
			out := gooserun.Run(t, dir, main, map[string][]byte{"corpus.bin": data})
			a := fixedAnswers(t, out, 1)[0]
			if a.n != 1 || a.reason != "ok" {
				t.Fatalf("%s: the Goose reader answered %d records, %s; want 1, ok", vec.File, a.n, a.reason)
			}
			if a.written != len(data) || a.bytes != hex.EncodeToString(data) {
				t.Fatalf("%s: the Goose writer's %d bytes differ from the stored %d:\n got %s\nwant %s", vec.File, a.written, len(data), a.bytes, hex.EncodeToString(data))
			}
		})
	}
}

// deprecatedWant is the text a manifest value prints as: a present companion
// is a bool, every other manifest value an integer.
func deprecatedWant(path string, value int64) string {
	if strings.HasSuffix(path, "_present") {
		return strconv.FormatBool(value != 0)
	}
	return strconv.FormatInt(value, 10)
}

// deprecatedExpr spells a manifest path (`r0.old_pick.spot.x`,
// `r0.old_marks[0]`) as the Goose expression for the same value.
func deprecatedExpr(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "r0.")
	if !ok || rest == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("values[0]")
	for _, seg := range strings.Split(rest, ".") {
		name, index := seg, ""
		if i := strings.IndexByte(seg, '['); i >= 0 {
			name, index = seg[:i], seg[i:]
		}
		if name == "" {
			return "", false
		}
		fmt.Fprintf(&b, ".%s%s", ir.GoExportName(name), index)
	}
	return b.String(), true
}
