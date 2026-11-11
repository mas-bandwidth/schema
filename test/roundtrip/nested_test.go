package roundtrip

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// nestedArms is the arm count of nested/Nested.schema's union: a rotation
// moves each arm kind through every element position.
const nestedArms = 4

// nestedSamples is the nested unit's samples, AT CAPACITY: every bounded array
// at its declared maximum. A box root (arms, pair, rows) gets one sample per
// rotation k: element e of arms (e = 0..3) and of pair (e = 4..5) holds arm
// (e + k) % 4, and every value is distinct by element and position, so each
// element's bytes are its own. A row root (xs) and the keyed record (per) get one full sample each. A root of
// any other shape is refused by name: the unit and this generator move
// together.
func nestedSamples(st *ir.Struct) (names, texts []string, err error) {
	fields := map[string]bool{}
	for _, f := range st.Fields {
		fields[f.Name] = true
	}
	switch {
	case len(fields) == 3 && fields["arms"] && fields["pair"] && fields["rows"]:
		for k := range nestedArms {
			names = append(names, fmt.Sprintf("rotate%d", k))
			texts = append(texts, nestedBox(k))
		}
		return names, texts, nil
	case len(fields) == 1 && fields["xs"]:
		return []string{"full"}, []string{`{"xs":[-32768,32767]}`}, nil
	case len(fields) == 1 && fields["per"]:
		return []string{"full"}, []string{`{"per":{"Left":-32768,"Right":32767}}`}, nil
	}
	return nil, nil, fmt.Errorf("root %s has a shape the nested unit's samples do not cover", st.Name)
}

// nestedArm is element e's arm under rotation k, every array in it full.
func nestedArm(e, k int) string {
	switch (e + k) % nestedArms {
	case 0:
		return fmt.Sprintf(`{"vals":[%d.5,%d.25]}`, 10*e+1, 10*e+2)
	case 1:
		label := make([]byte, 8)
		for j := range label {
			label[j] = byte('a' + e + 3*j)
		}
		return fmt.Sprintf(`{"label":%q}`, label)
	case 2:
		return fmt.Sprintf(`{"nums":[%d,%d,%d]}`, 100*e+1, 100*e+2, 100*e+3)
	default:
		return fmt.Sprintf(`{"sides":{"per":{"Left":%d,"Right":%d}}}`, 1000*e+1, -(1000*e + 2))
	}
}

func nestedBox(k int) string {
	var b strings.Builder
	b.WriteString(`{"arms":[`)
	for e := range 4 {
		if e > 0 {
			b.WriteString(",")
		}
		b.WriteString(nestedArm(e, k))
	}
	b.WriteString(`],"pair":[`)
	for e := 4; e < 6; e++ {
		if e > 4 {
			b.WriteString(",")
		}
		b.WriteString(nestedArm(e, k))
	}
	b.WriteString(`],"rows":[`)
	for e := range 3 {
		if e > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"xs":[%d,%d]}`, 10*e+k+1, 10*e+k+2)
	}
	b.WriteString(`]}`)
	return b.String()
}

// TestNestedSamplesAtCapacity holds the box samples to their promise, read back
// from their own text: every arm kind at every element position of arms and
// pair, and every counted array at its bound.
func TestNestedSamplesAtCapacity(t *testing.T) {
	type box struct {
		Arms []map[string]json.RawMessage `json:"arms"`
		Pair []map[string]json.RawMessage `json:"pair"`
		Rows []struct {
			Xs []int `json:"xs"`
		} `json:"rows"`
	}
	seen := map[int]map[string]bool{}
	for k := range nestedArms {
		var b box
		if err := json.Unmarshal([]byte(nestedBox(k)), &b); err != nil {
			t.Fatalf("rotate%d: %v", k, err)
		}
		if len(b.Arms) != 4 || len(b.Pair) != 2 || len(b.Rows) != 3 {
			t.Fatalf("rotate%d: arms %d, pair %d, rows %d; want 4, 2, 3", k, len(b.Arms), len(b.Pair), len(b.Rows))
		}
		for _, r := range b.Rows {
			if len(r.Xs) != 2 {
				t.Errorf("rotate%d: a row holds %d xs; want 2", k, len(r.Xs))
			}
		}
		for pos, arm := range append(b.Arms, b.Pair...) {
			if seen[pos] == nil {
				seen[pos] = map[string]bool{}
			}
			for name := range arm {
				seen[pos][name] = true
			}
		}
	}
	for pos := range 6 {
		if len(seen[pos]) != nestedArms {
			t.Errorf("position %d holds %d of the %d arm kinds across the rotations", pos, len(seen[pos]), nestedArms)
		}
	}
}
