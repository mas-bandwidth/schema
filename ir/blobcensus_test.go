package ir_test

// THE BYTE BUFFER CENSUS (docs/SPEC-TABLES.md §2.5) and the one property a
// backend gates a DEFINITION on it for: it is a superset of the numbering
// walk's reachability answer, so a unit whose emitter has already written a
// call to the byte buffer runtime cannot be a unit the census withholds it
// from.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// every shape that DECLARES a byte buffer pointer: a pointee's field, a union
// arm, and a map's value (a LIST of byte buffers is refused outright, §15).
const blobCensusSource = `package demo

table Leaf { data *bytes }

union Reach
{
    text  *string
    plain int32
}

table Gate
{
    reach Reach
    after int32
}

table Holder
{
    leaf    *Leaf
    tags    map[uint32]*bytes
    ordinal int32
}
`

func TestBlobPointerFieldsNamesEveryDeclaration(t *testing.T) {
	got := ir.BlobPointerFields(unitFrom(t, blobCensusSource))
	want := []string{
		"HolderTagsEntry.value",
		"Leaf.data",
		"Reach.text",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BlobPointerFields = %v, want %v", got, want)
	}
}

// THE PROPERTY the C++ emitter's dead-output gate rests on: whatever root the
// numbering walk is taken from, every byte buffer it reaches is a declaration
// this census already names. A backend that gates a DEFINITION on the census
// therefore cannot lose to its own emitter's walk, however the two disagree.
func TestBlobPointerFieldsIsASupersetOfEveryRootsReachableSet(t *testing.T) {
	u := unitFrom(t, blobCensusSource)
	named := map[string]bool{}
	for _, q := range ir.BlobPointerFields(u) {
		named[q] = true
	}
	if len(named) == 0 {
		t.Fatal("the fixture declares byte buffers and the census found none")
	}
	roots := 0
	for name, st := range u.Tables {
		bytes, str := ir.PointerReachableBlobs(st)
		if !bytes && !str {
			continue
		}
		roots++
		// every declaration the walk from this root could have answered with
		// is one the census names, so the reachable set is inside it
		for _, q := range reachedBlobDeclarations(t, u, st) {
			if !named[q] {
				t.Errorf("%s reaches %s and the unit's census does not name it", name, q)
			}
		}
	}
	if roots == 0 {
		t.Fatal("no root of the fixture reaches a byte buffer, so the property was not asked")
	}
}

// reachedBlobDeclarations is the qualified name of every byte buffer the
// numbering walk from root actually visits, taken the only way a test can take
// it: [ir.PointerReachableBlobs] answers in kinds, so the declarations are
// matched back by kind over the whole census.
func reachedBlobDeclarations(t *testing.T, u *ir.Unit, root *ir.Struct) []string {
	t.Helper()
	bytes, str := ir.PointerReachableBlobs(root)
	var out []string
	for _, q := range ir.BlobPointerFields(u) {
		if k := blobKindOf(u, q); (k == ir.TBytes && bytes) || (k == ir.TString && str) {
			out = append(out, q)
		}
	}
	return out
}

// blobKindOf is the kind of the declaration the census named, TInt when the
// name resolves to nothing (which the caller's fixture never does).
func blobKindOf(u *ir.Unit, qualified string) ir.FieldTypeKind {
	dot := strings.LastIndex(qualified, ".")
	owner, member := qualified[:dot], qualified[dot+1:]
	if st := u.Tables[owner]; st != nil {
		for _, f := range st.Fields {
			if f.Name == member {
				return f.Type.Kind
			}
		}
	}
	if st := u.Structs[owner]; st != nil {
		for _, f := range st.Fields {
			if f.Name == member {
				return f.Type.Kind
			}
		}
	}
	for _, unions := range []map[string]*ir.Union{u.Unions, u.TableUnions} {
		if un := unions[owner]; un != nil {
			for _, v := range un.Variants {
				if v.Name == member && v.F != nil {
					return v.F.Type.Kind
				}
			}
		}
	}
	return ir.TInt // not a byte buffer: no reachable set answers with it
}

// the nearest neighbour: the same pointered unit with every blob pointer
// respelled a table pointer carries no declaration at all.
const blobCensusNodeSource = `package demo

fixed table Leaf { v int32 }

union Reach
{
    only  *Leaf
    plain int32
}

table Gate
{
    reach Reach
    after int32
}
`

func TestBlobPointerFieldsIsEmptyWithoutADeclaration(t *testing.T) {
	if got := ir.BlobPointerFields(unitFrom(t, blobCensusNodeSource)); len(got) != 0 {
		t.Errorf("a unit with no byte buffer named %v", got)
	}
}
