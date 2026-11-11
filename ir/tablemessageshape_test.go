// AN ARRAY OR KEYED SHAPE'S ELEMENT IS NEVER AN ARRAY OR A KEYED SHAPE
// (docs/SPEC-TABLES.md §3.3): no declaration produces one, and an announcement
// carrying one is refused before the shape decoder recurses into it.
package ir

import (
	"testing"
)

func arrayShape(min, max int64, elem uint8, inner TableMessageShape) TableMessageShape {
	return TableMessageShape{Min: min, Max: max, Elem: elem, Inner: &inner}
}

func keyedShape(slots int64, elem uint8, inner TableMessageShape) TableMessageShape {
	return TableMessageShape{Max: slots, Elem: elem, Inner: &inner}
}

func scalarShape() TableMessageShape { return TableMessageShape{Packing: TableMessageRaw} }

func TestAnnouncedListElementIsNotAList(t *testing.T) {
	t.Parallel()
	scalar := scalarShape()
	table := TableMessageShape{}
	text := TableMessageShape{Max: 8}
	array := arrayShape(0, 4, TableKindU8, scalar)
	keyed := keyedShape(4, TableKindU8, scalar)

	refused := []struct {
		name  string
		kind  uint8
		shape TableMessageShape
	}{
		{"array over array", TableKindArray, arrayShape(0, 4, TableKindArray, array)},
		{"array over keyed", TableKindArray, arrayShape(0, 4, TableKindKeyed, keyed)},
		{"keyed over array", TableKindKeyed, keyedShape(4, TableKindArray, array)},
		{"keyed over keyed", TableKindKeyed, keyedShape(4, TableKindKeyed, keyed)},
	}
	nested := array
	for i := 0; i < 1000; i++ {
		nested = arrayShape(0, 4, TableKindArray, nested)
	}
	refused = append(refused, struct {
		name  string
		kind  uint8
		shape TableMessageShape
	}{"a 1000-level nest", TableKindArray, nested})

	for _, tc := range refused {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := TableVocabularyEntry{Id: 1, Kind: tc.kind, Shape: tc.shape}
			encoded := entry.Encode(nil)
			if _, _, ok := DecodeShape(encoded[9:], tc.kind); ok {
				t.Errorf("%s: DecodeShape accepted an element kind of %d", tc.name, tc.shape.Elem)
			}
		})
	}

	decoded := []struct {
		name  string
		kind  uint8
		shape TableMessageShape
	}{
		{"array over a string", TableKindArray, arrayShape(0, 4, TableKindString, text)},
		{"array over a scalar", TableKindArray, arrayShape(0, 4, TableKindU32, scalar)},
		{"keyed over a table", TableKindKeyed, keyedShape(4, TableKindTable, table)},
	}
	for _, tc := range decoded {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := TableVocabularyEntry{Id: 1, Kind: tc.kind, Shape: tc.shape}
			encoded := entry.Encode(nil)
			if _, _, ok := DecodeShape(encoded[9:], tc.kind); !ok {
				t.Errorf("%s: DecodeShape refused a lawful element kind of %d", tc.name, tc.shape.Elem)
			}
		})
	}
}
