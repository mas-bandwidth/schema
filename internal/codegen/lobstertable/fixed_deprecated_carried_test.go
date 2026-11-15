package lobstertable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedDeprecatedFieldIsCarried pins the fixed form's deprecated-field
// rule (docs/SPEC-TABLES.md §3.4, §21.2, §22.6; docs/rulings.json
// fixed-deprecated-field-is-carried): a deprecated field is CARRIED. It reads
// and writes like any other field, so each shared vector lands the value the
// writer stored, not the declared default, and saves back byte for byte.
func TestFixedDeprecatedFieldIsCarried(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain and the C++ fixed-form corpus")
	corpus := fixedCorpusDir(t)
	for _, vec := range fixedvector.DeprecatedField {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused {
				t.Fatalf("%s: the manifest line carries no expect=, so the reference did not land it", vec.File)
			}
			wire, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatal(err)
			}
			u := deprecatedUnit(t, vec.Schema)
			count, reason, fields, resaved, _ := lobsterLoadLineage(t, u, vec.Root, nil, wire, -1, nil)
			if count < 0 {
				t.Fatalf("%s: FixedLoad refused the stored record: %s", vec.File, reason)
			}
			if count != 1 {
				t.Fatalf("%s: FixedLoad landed %d records, want 1", vec.File, count)
			}
			for path, v := range want.Values {
				if got, ok := fields[path]; !ok || got != strconv.FormatInt(v, 10) {
					t.Errorf("%s: %s landed %q (present %v), want the stored %d", vec.File, path, got, ok, v)
				}
			}
			for path, text := range want.Texts {
				if got, ok := fields[path]; !ok || got != strconv.Quote(text) {
					t.Errorf("%s: %s landed %q (present %v), want the stored %q", vec.File, path, got, ok, text)
				}
			}
			if !bytes.Equal(resaved, wire) {
				t.Errorf("%s: save after load is not the stored bytes (got %d, want %d)", vec.File, len(resaved), len(wire))
			}
		})
	}
}

// deprecatedUnit loads the test/tables schema a deprecated-field vector names.
func deprecatedUnit(t *testing.T, schema string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "test", "tables", schema+".schema")})
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s does not load: %v", schema, err)
	}
	return u
}
