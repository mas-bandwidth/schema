package mootable_test

// A DEPRECATED FIELD IN THE FIXED FORM IS CARRIED (docs/SPEC-TABLES.md §3.4,
// §21.2, §22.6; docs/rulings.json fixed-deprecated-field-is-carried). Each
// shared vector (fixedvector.DeprecatedField) holds a lawful value in a
// deprecated field that is not its declared default: the emitted reader lands
// that stored value as it lands any field, and the writer saves the record back
// byte for byte. The declared default applies only where the record does not
// supply the field.

import (
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, vec := range fixedvector.DeprecatedField {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused {
				t.Fatalf("the C++ reference refused %s: %+v", vec.File, want)
			}
			file := corpusFile(t, corpus, vec.File)
			g := fixedUnit(t, readTestSchema(t, vec.Schema), nil)
			got := runFixedLoad(t, g, vec.Schema+"Table", vec.Root, file)
			if got.head != "ok 1 u0 k0 c0 w0" {
				t.Fatalf("%s: %s, want the one record read", vec.File, got.head)
			}
			for p, v := range want.Values {
				if got.dump[p] != strconv.FormatInt(v, 10) {
					t.Errorf("%s: %s landed %q, the reference stored %d", vec.File, p, got.dump[p], v)
				}
			}
			for p, text := range want.Texts {
				if got.dump[p] != `"`+text+`"` {
					t.Errorf("%s: %s landed %q, the reference stored %q", vec.File, p, got.dump[p], text)
				}
			}
			if string(got.saved) != string(file) {
				t.Errorf("%s: the save is not the reference's bytes: %d saved, %d in the file; %s", vec.File, len(got.saved), len(file), firstDiff(got.saved, file))
			}
		})
	}
}
