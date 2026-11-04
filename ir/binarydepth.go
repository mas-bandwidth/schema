package ir

import "fmt"

// TableBinaryDepth is the deepest framed level a body of st reaches on the
// variable form (docs/SPEC-TABLES.md §4, `binary_max_depth`): its own body is
// level 0, and each table, array, keyed or map body inside it one level
// deeper. A pointer does not nest, since its node record is walked from level
// 0 as a root of its own. It is what [TableBinaryMaxDepth] bounds.
func TableBinaryDepth(st *Struct) int {
	return newBinaryDepth().body(st)
}

type binaryDepth struct {
	memo map[*Struct]int
	open map[*Struct]bool
}

func newBinaryDepth() *binaryDepth {
	return &binaryDepth{memo: map[*Struct]int{}, open: map[*Struct]bool{}}
}

func (d *binaryDepth) body(st *Struct) int {
	if st == nil {
		return 0
	}
	if n, ok := d.memo[st]; ok {
		return n
	}
	if d.open[st] {
		return 0 // a by-value cycle is the cycle check's refusal, not this one's
	}
	d.open[st] = true
	deepest := 0
	for i := range st.Fields {
		deepest = max(deepest, d.field(st.Fields[i]))
	}
	delete(d.open, st)
	d.memo[st] = deepest
	return deepest
}

// field is the deepest level a field's payload reaches below the body that
// holds it.
func (d *binaryDepth) field(f *Field) int {
	if f == nil {
		return 0
	}
	if f.IsMap() {
		return 2 + d.body(f.MapEntry) // the map body, then each entry's body
	}
	if TextListPlain(f) {
		return 1 // kind 12 elements in the array body
	}
	if f.Array != ArrayNone || f.KeyEnum != "" {
		return 1 + d.value(f)
	}
	return d.value(f)
}

// value is the deepest level one value of the field's type reaches below the
// payload holding it.
func (d *binaryDepth) value(f *Field) int {
	if f.Type.Pointer || f.Type.Kind != TNamed {
		return 0
	}
	switch ref := f.Type.Ref.(type) {
	case *Struct:
		return 1 + d.body(ref)
	case *Union:
		deepest := 0
		for i := range ref.Variants {
			deepest = max(deepest, d.field(ref.Variants[i].F))
		}
		return deepest
	}
	return 0
}

// CheckBinaryDepth refuses every table or type of the unit whose body nests
// past [TableBinaryMaxDepth], so no conforming writer produces an input the
// read's bound refuses (docs/SPEC-TABLES.md §4).
func CheckBinaryDepth(u *Unit) []SizeError {
	var out []SizeError
	d := newBinaryDepth()
	for _, name := range sizeCheckOrder(u) {
		st := memberStruct(u, name)
		if st == nil {
			continue
		}
		if n := d.body(st); n > TableBinaryMaxDepth {
			kind := "type"
			if st.IsTable {
				kind = "table"
			}
			out = append(out, SizeError{Owner: st.Name, OwnerKind: kind,
				Detail: fmt.Sprintf("nests %d framed levels deep by value, past the binary bound of %d (docs/SPEC-TABLES.md §4, binary_max_depth)", n, TableBinaryMaxDepth)})
		}
	}
	return out
}
