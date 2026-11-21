package ir_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const vocabularyNameSource = `package vocabname

enum Grade
{
    Bronze,
    Argent | was = "Silver"
    Gold
}

type Ward
{
    charge float32 = 0.0
}

union Effect
{
    shield Ward  | was = "ward"
    pong         | was = "ping"
    amount int32
}

table Node
{
    n int32
}

union Slot
{
    vertex *Node | was = "node"
    plain int32
}

table Alpha
{
    x     int32 | was = "y"
    grade Grade
    eff   Effect
    slot  Slot
}

table Beta
{
    y int32
}
`

// TestTableVocabularyNameResolvesWireNames pins docs/SPEC-TABLES.md §5 on the
// name lookup: a variant or arm renamed under `was` is announced under its OLD
// name's hash, so that is the id the unit names it by. An arm of a union with
// a table arm (u.TableUnions, absent from u.Unions) is named too.
func TestTableVocabularyNameResolvesWireNames(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, vocabularyNameSource)
	tests := []struct {
		wire, want string
	}{
		{"Silver", "Argent"},
		{"ward", "shield"},
		{"ping", "pong"},
		{"node", "vertex"},
		{"plain", "plain"},
		{"amount", "amount"},
	}
	for _, tc := range tests {
		if got := ir.TableVocabularyName(u, ir.TableWireId(tc.wire)); got != tc.want {
			t.Errorf("TableVocabularyName(hash(%q)) = %q, want %q", tc.wire, got, tc.want)
		}
	}
	// Every id the unit's own vocabulary announces is one it can name.
	for _, e := range ir.TableVocabulary(u) {
		if ir.TableVocabularyName(u, e.Id) == "" {
			t.Errorf("announced id %016x names nothing", e.Id)
		}
	}
}

// TestTableVocabularyNameIsDeterministic holds the lookup to one answer when
// two members name the same id: Alpha.x rides under "y" and Beta.y is "y".
// The walk is in sorted member order, so Alpha's field answers, every run.
func TestTableVocabularyNameIsDeterministic(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, vocabularyNameSource)
	id := ir.TableWireId("y")
	for i := 0; i < 64; i++ {
		if got := ir.TableVocabularyName(u, id); got != "x" {
			t.Fatalf("run %d: TableVocabularyName(hash(\"y\")) = %q, want \"x\" (Alpha sorts before Beta)", i, got)
		}
	}
}
