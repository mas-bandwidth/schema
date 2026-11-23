package ir_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const fixedBoundsSchema = `package demo

fixed table Child
{
    vals [2..4]uint8
}

fixed table Free
{
    vals [..4]uint8
}

fixed table Loose { nest [..4]Child }
fixed table Tight { nest [1..4]Child }
fixed table Own { nest [1..4]Free }
fixed table Wrap { inner Child }
fixed table Deep { outer [..2]Wrap }
fixed table Pair { two [2]Child }

union Pick
{
    child Child
    free  Free
}

fixed table Arm { pick Pick }
fixed table Arms { picks [..3]Pick }
fixed table Plain { nest [..4]Free }
`

// A counted array's minimum one record down is a bound of the record that
// holds it, whatever the holding array's own minimum (docs/SPEC.md §5.1 row
// 3; F-184, F-187): the walk reaches it through an array of records, a record
// in a record, an array of records that each hold a record, a fixed array, a
// union arm's payload and an array of unions. A record whose only bound is
// its own array's minimum walks no element, and a record with no bound at all
// needs no pass.
func TestTableFixedBoundsReachACountMinimumAtAnyDepth(t *testing.T) {
	u := unitFrom(t, fixedBoundsSchema)
	record := func(table string) *ir.Struct {
		t.Helper()
		st := u.Tables[table]
		if st == nil {
			t.Fatalf("no table %s", table)
		}
		return st
	}
	field := func(table string) *ir.Field {
		t.Helper()
		return record(table).Fields[0]
	}
	for _, c := range []struct {
		table          string
		needed, walked bool
	}{
		{"Child", true, false},
		{"Free", false, false},
		{"Loose", true, true},
		{"Tight", true, true},
		{"Own", true, false},
		{"Wrap", true, true},
		{"Deep", true, true},
		{"Pair", true, true},
		{"Arm", true, true},
		{"Arms", true, true},
		{"Plain", false, false},
	} {
		f := field(c.table)
		if got := ir.TableFixedBoundsNeeded(record(c.table)); got != c.needed {
			t.Errorf("%s: TableFixedBoundsNeeded = %v, want %v", c.table, got, c.needed)
		}
		if got := ir.TableFixedBoundsNeededField(f); got != c.needed {
			t.Errorf("%s.%s: TableFixedBoundsNeededField = %v, want %v", c.table, f.Name, got, c.needed)
		}
		if got := ir.TableFixedElementBoundsNeeded(f); got != c.walked {
			t.Errorf("%s.%s: TableFixedElementBoundsNeeded = %v, want %v", c.table, f.Name, got, c.walked)
		}
	}
	pick := u.Unions["Pick"]
	if pick == nil {
		pick = u.TableUnions["Pick"]
	}
	if pick == nil {
		t.Fatal("no union Pick")
	}
	if !ir.TableFixedBoundsNeededUnionArm(pick) {
		t.Errorf("Pick: an arm holds Child's minimum and TableFixedBoundsNeededUnionArm says none does")
	}
}
