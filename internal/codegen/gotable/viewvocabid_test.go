package gotable

import "testing"

// TestViewIdsKeyOnTheVocabulary pins docs/SPEC-TABLES.md §8.2: a field's
// VariantId and KeyId are keyed on the VOCABULARY a table closure reaches,
// not on the field's owner. A table reaches E, so E keeps its checked ids
// everywhere, including on a type outside every table closure; F, reached by
// no table closure, answers 0 even on that same outside type.
func TestViewIdsKeyOnTheVocabulary(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
enum E { A, B }
enum F { C, D }
type Outside {
 e E
 f F
 kept [E]int32
 lost [F]int32
}
table Root {
 e E
 kept [E]int32
}
`, `package probe
import "testing"
func TestViewIds(t *testing.T){
 want1,_:=E(1).TableEnumId()
 want2,_:=E(2).TableEnumId()
 out:=OutsideTableType()
 if got:=out.Fields[0].VariantId(1);got!=want1{t.Fatalf("outside E variant 1 = %#x, want %#x",got,want1)}
 if got:=out.Fields[0].VariantId(2);got!=want2{t.Fatalf("outside E variant 2 = %#x, want %#x",got,want2)}
 if got:=out.Fields[1].VariantId(1);got!=0{t.Fatalf("outside F variant 1 = %#x, want 0",got)}
 if got:=out.Fields[1].VariantId(2);got!=0{t.Fatalf("outside F variant 2 = %#x, want 0",got)}
 if got:=out.Fields[2].KeyId(1);got!=want1{t.Fatalf("outside keyed E key 1 = %#x, want %#x",got,want1)}
 if got:=out.Fields[3].KeyId(1);got!=0{t.Fatalf("outside keyed F key 1 = %#x, want 0",got)}
 if got:=RootTableFields[0].VariantId(1);got!=want1{t.Fatalf("table E variant 1 = %#x, want %#x",got,want1)}
 if got:=RootTableFields[1].KeyId(1);got!=want1{t.Fatalf("table keyed E key 1 = %#x, want %#x",got,want1)}
 }
 `)
}
