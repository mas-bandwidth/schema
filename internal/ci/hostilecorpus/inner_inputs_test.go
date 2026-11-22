package hostilecorpus

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A BOUND IS HELD ONE LEVEL DOWN, WHATEVER THE PARENT DECLARES.
//
// test/tables/inner declares four children, each with exactly one thing a
// reader holds (a counted array's minimum, an integer range, a string
// capacity, a bytes capacity), and for each a parent that is an array of that
// child and nothing else: once with no minimum of its own (Loose), once with a
// minimum of one (Tight), on a fixed root and on a variable root. The fixed
// roots are read in the fixed form and in the message form, the variable
// roots in the variable form.
//
// Every row below is one parent that is itself within its bounds, holding one
// child. The `held` rows carry a child outside its one bound and every reader
// refuses them; each has a twin at the bound that every reader accepts, so a
// refusal is the bound's and never the framing's. No writer makes a `held`
// row: the variable and message images are the engine's own encoding of the
// same value under a declaration without that bound (and the twin's bytes are
// the same under both declarations), and the fixed image is the twin with the
// one stored number changed.

const innerSchemaPath = "test/tables/inner"

// innerKind is one child bound: the value at the bound, the value one past
// it, and the change to the declaration that admits the second.
type innerKind struct {
	kind         string // the parent tables' suffix
	at, past     string // the row names' tails
	atText       string // one child at its bound
	pastText     string // one child one past it
	tight, loose string // the child's declaration, and the one without the bound
	detail       string // the engine's own name for the refusal
}

// innerMessageCount is the count bound the MESSAGE form can carry. A count
// rides there as its offset from the minimum (docs/SPEC-TABLES.md §3.3), so
// under the reader's own announcement a count below the minimum has no
// spelling, and the count outside its bounds is the one past the maximum:
// [2..4] is two bits, and the fourth offset is a count of five.
var innerMessageCount = innerKind{"Count", "count-at-max", "count-past-max", `{"vals":[7,0,0,0]}`, `{"vals":[7,0,0,0,0]}`,
	"[2..4]uint8", "[2..5]uint8", "count_over_bound"}

var innerKinds = []innerKind{
	{"Count", "count-at-min", "count-below-min", `{"vals":[7,0]}`, `{"vals":[7]}`,
		"[2..4]uint8", "[..4]uint8", "count_over_bound"},
	{"Range", "range-at-max", "range-past-max", `{"level":9}`, `{"level":10}`,
		"min = 1, max = 9", "min = 1, max = 16", "out_of_range"},
	{"Text", "text-at-capacity", "text-past-capacity", `{"word":"abcd"}`, `{"word":"abcde"}`,
		"string(4)", "string(7)", "length_over_bound"},
	{"Bytes", "bytes-at-capacity", "bytes-past-capacity", `{"blob":"YWJjZA=="}`, `{"blob":"YWJjZGU="}`,
		"bytes(4)", "bytes(7)", "length_over_bound"},
}

// innerForms is the three forms: the row names' form, the carrier, and the
// root tables' prefix.
var innerForms = []struct {
	form, carrier, root string
}{
	{"fixed", "wire", "Fixed"},
	{"variable", "wire", "Var"},
	{"message", "message", "Fixed"},
}

type innerRow struct {
	name, carrier, table, verdict, detail, provenance string
	payload                                           []byte
}

func innerLooseModel(t *testing.T, tight string, kinds []innerKind) *tabletext.Model {
	t.Helper()
	text := tight
	for _, k := range kinds {
		if strings.Count(text, k.tight) != 1 {
			t.Fatalf("the inner schema does not declare %q exactly once", k.tight)
		}
		text = strings.Replace(text, k.tight, k.loose, 1)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Inner.schema"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadModel(dir, dir, "Inner.schema")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func innerPlace(t *testing.T, m *tabletext.Model, table, text string) *tabletext.Instance {
	t.Helper()
	st := m.Lookup(table)
	if st == nil {
		t.Fatalf("%s is not in the inner schema", table)
	}
	inst := m.New(st)
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) || !r.Silent() {
		t.Fatalf("%s: %s did not place cleanly: %+v", table, text, r)
	}
	return inst
}

func innerEncode(t *testing.T, m *tabletext.Model, carrier, table, text string) []byte {
	t.Helper()
	inst := innerPlace(t, m, table, text)
	var out []byte
	var err error
	if carrier == "message" {
		out, err = tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
	} else {
		out, err = tablewire.Encode(m, inst)
	}
	if err != nil {
		t.Fatalf("%s %s: %v", table, text, err)
	}
	return out
}

// innerFixedPast is the fixed image of the twin with the one stored number
// that carries the bound moved one past it. The number's place is found, not
// assumed: it is the one byte at which the twin and a second lawful image
// (the same child one step inside its bound) differ by exactly the step.
func innerFixedPast(t *testing.T, m *tabletext.Model, table, kind string) []byte {
	t.Helper()
	var inside string
	var step int // the stored number at the bound minus the one inside it
	switch kind {
	case "Count":
		inside, step = `{"vals":[7,0,0]}`, -1
	case "Range":
		inside, step = `{"level":8}`, 1
	case "Text":
		inside, step = `{"word":"abc"}`, 1
	case "Bytes":
		inside, step = `{"blob":"YWJj"}`, 1
	}
	var at string
	for _, k := range innerKinds {
		if k.kind == kind {
			at = k.atText
		}
	}
	a := innerEncode(t, m, "wire", table, `{"nest":[`+at+`]}`)
	b := innerEncode(t, m, "wire", table, `{"nest":[`+inside+`]}`)
	if len(a) != len(b) {
		t.Fatalf("%s: two fixed images of one type differ in length", table)
	}
	place := -1
	for i := range a {
		if int(a[i])-int(b[i]) == step {
			if place >= 0 {
				t.Fatalf("%s: the stored number is not at exactly one byte", table)
			}
			place = i
		}
	}
	if place < 0 {
		t.Fatalf("%s: the two images do not differ at the stored number", table)
	}
	out := append([]byte(nil), a...)
	out[place] = byte(int(a[place]) + step)
	return out
}

func innerRows(t *testing.T) []innerRow {
	t.Helper()
	root := moduleRoot(t)
	tight, err := LoadModel(root, t.TempDir(), innerSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, innerSchemaPath, "Inner.schema"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []innerRow
	for _, f := range innerForms {
		kinds := innerKinds
		if f.form == "message" {
			kinds = append([]innerKind{innerMessageCount}, innerKinds[1:]...)
			// a bytes(N)'s used length is a count in the message form, and
			// the engine names it as one
			kinds[3].detail = "count_over_bound"
		}
		loose := innerLooseModel(t, string(text), kinds)
		for _, parent := range []string{"Loose", "Tight"} {
			for _, k := range kinds {
				table := f.root + parent + k.kind
				stem := "inner-" + f.form + "-" + strings.ToLower(parent) + "-"
				atText := `{"nest":[` + k.atText + `]}`
				at := innerEncode(t, tight, f.carrier, table, atText)
				var past []byte
				if f.form == "fixed" {
					past = innerFixedPast(t, tight, table, k.kind)
				} else {
					// the same value written under the declaration without
					// the bound, whose bytes for the twin are the tight
					// declaration's own
					if same := innerEncode(t, loose, f.carrier, table, atText); !bytes.Equal(same, at) {
						t.Fatalf("%s%s: the %s form's bytes depend on the bound, so the past-the-bound image is not this declaration's", stem, k.at, f.form)
					}
					past = innerEncode(t, loose, f.carrier, table, `{"nest":[`+k.pastText+`]}`)
				}
				parentSays := "declares no minimum"
				if parent == "Tight" {
					parentSays = "holds its own minimum of one"
				}
				rows = append(rows,
					innerRow{stem + k.at, f.carrier, table, Accept, "",
						fmt.Sprintf("inner bound, %s form: one child at its bound in a parent array that %s (test/tables/inner; TestInnerBoundsInputsArePinned)", f.form, parentSays), at},
					innerRow{stem + k.past, f.carrier, table, Refuse, k.detail,
						fmt.Sprintf("inner bound, %s form: one child one past its bound in a parent array that %s and is itself within its bounds (test/tables/inner; TestInnerBoundsInputsArePinned)", f.form, parentSays), past})
			}
		}
	}
	return rows
}

// innerDetail is the engine's own name for a refusal, which the manifest's
// one `damaged` does not carry.
func innerDetail(m *tabletext.Model, carrier, table string, payload []byte) string {
	inst := m.New(m.Lookup(table))
	var r tabletext.Report
	var err error
	if carrier == "message" {
		v := &tablewire.Vocabulary{}
		var ar tabletext.Report
		if aerr := v.AnnounceRead(tablewire.Announce(m.Unit), &ar); aerr != nil {
			return "announce: " + aerr.Error()
		}
		_, _, err = tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, payload, v, &r)
	} else {
		_, err = tablewire.Decode(m, inst, payload, &r)
	}
	if fr, ok := errors.AsType[*tablewire.FixedRefusal](err); ok {
		return fr.Reason
	}
	if err != nil {
		return r.Reason + " (" + err.Error() + ")"
	}
	return r.Reason
}

// TestInnerBoundsInputsArePinned holds the committed inner-bound inputs to
// their generator, each row's verdict to the engine's, and each refusal to
// the bound it is about. INNER_WRITE=1 writes the inputs and the rows.
func TestInnerBoundsInputsArePinned(t *testing.T) {
	root := moduleRoot(t)
	dir := filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus")
	tight, err := LoadModel(root, t.TempDir(), innerSchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := innerRows(t)
	if len(rows) != 48 {
		t.Fatalf("%d rows, want 48: three forms, two parents, four bounds, at and past", len(rows))
	}
	write := os.Getenv("INNER_WRITE") == "1"
	manifest, committed, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Case{}
	for _, c := range committed {
		byName[c.Name] = c
	}
	var fresh []Case
	for _, row := range rows {
		ext := ".bin"
		c := Case{Name: row.name, Carrier: row.carrier, Schema: innerSchemaPath, Table: row.table,
			Input: "inputs/" + row.name + ext, Provenance: row.provenance}
		verdict, reason, digest, err := EngineVerdict(tight, c, row.payload)
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		c.Verdict, c.Reason, c.Digest = verdict, reason, digest
		if verdict != row.verdict {
			t.Errorf("%s: the engine answers %s %s, want %s", row.name, verdict, reason, row.verdict)
		}
		if row.verdict == Refuse {
			if reason != ReasonDamaged {
				t.Errorf("%s: refused %s, want %s", row.name, reason, ReasonDamaged)
			}
			if got := innerDetail(tight, row.carrier, row.table, row.payload); !strings.Contains(got, row.detail) {
				t.Errorf("%s: the engine refuses for %q, and the row is about %s", row.name, got, row.detail)
			}
		}
		fresh = append(fresh, c)
		path := filepath.Join(dir, "inputs", row.name+ext)
		if write {
			if err := os.WriteFile(path, row.payload, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v (run with INNER_WRITE=1 to write it)", row.name, err)
			continue
		}
		if !bytes.Equal(got, row.payload) {
			t.Errorf("%s: the committed input is not the generator's bytes (run with INNER_WRITE=1)", row.name)
		}
		if have, ok := byName[row.name]; !ok {
			t.Errorf("%s: the manifest has no such row (run with INNER_WRITE=1)", row.name)
		} else if have != c {
			t.Errorf("%s: the manifest row is not the engine's:\n  committed: %+v\n  engine:    %+v", row.name, have, c)
		}
	}
	if !write {
		return
	}
	if Format(committed) != manifest {
		t.Fatal("the committed manifest is not in its own format, so it is not rewritten")
	}
	var merged []Case
	for _, c := range committed {
		if !strings.HasPrefix(c.Name, "inner-") {
			merged = append(merged, c)
		}
	}
	merged = append(merged, fresh...)
	if err := WriteManifest(dir, Format(merged)); err != nil {
		t.Fatal(err)
	}
}
