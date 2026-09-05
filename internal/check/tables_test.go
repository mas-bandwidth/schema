// Tests for the table frontend (docs/SPEC-TABLES.md): the refusals that keep the
// table wire sound, and the independence proof — tables move no protocol id.
package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func buildUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("T.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := Unit([]SourceFile{{
		Path: "T.schema", Name: "T.schema", Base: "T", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// TestTableRefusals is the tables extension of the break-the-language suite:
// every way to misuse a table that the checker provably catches.
func TestTableRefusals(t *testing.T) {
	cases := []struct {
		name string
		want string
		src  string
	}{
		{name: "a type field cannot reference a table", want: "is a table, not a wire type",
			src: "package t\ntable Tab { x int32 }\ntype P { tab Tab }\n"},
		{name: "was on a type field is refused by name", want: "was is a table-wire concept",
			src: "package t\ntype P { speed float32 | was = \"velocity\" }\n"},
		{name: "was naming the field's own current name", want: "names the field's own current name",
			src: "package t\ntable Tab { speed float32 | was = \"speed\" }\n"},
		{name: "was with an empty string", want: "names nothing",
			src: "package t\ntable Tab { speed float32 | was = \"\" }\n"},
		{name: "was takes a quoted string", want: "was takes the field's old name as a quoted string",
			src: "package t\ntable Tab { speed float32 | was = velocity }\n"},
		{name: "json outside a table closure is refused by name", want: "the text form is the table closure's",
			src: "package t\ntype P { a int32 | json = \"b\" }\n"},
		{name: "json keys collide inside one table", want: "collide on the JSON key",
			src: "package t\ntable Tab {\n    a int32 | json = \"b\"\n    b int32\n}\n"},
		{name: "two json attributes naming one key", want: "collide on the JSON key",
			src: "package t\ntable Tab {\n    a int32 | json = \"k\"\n    b int32 | json = \"k\"\n}\n"},
		{name: "json takes a quoted string", want: "json takes the field's text key as a quoted string",
			src: "package t\ntable Tab { a int32 | json = b }\n"},
		{name: "json with an empty string", want: "json = \"\" names nothing",
			src: "package t\ntable Tab { a int32 | json = \"\" }\n"},
		// the `&` prefix is the text form's (§16.7): `&node` is how a
		// shared node is named, so no field may take a key that begins with it
		{name: "json key with the reserved prefix", want: "a key beginning with `&` is reserved to the text form",
			src: "package t\ntable Tab { a int32 | json = \"&a\" }\n"},
		{name: "effective wire ids collide (was aliases a live sibling)", want: "collide on table-wire id",
			src: "package t\ntable Tab {\n    a int32 | was = \"b\"\n    b int32\n}\n"},
		{name: "a table nesting itself is a composition cycle", want: "type composition cycle",
			src: "package t\ntable Tab { inner Tab }\n"},
		{name: "a table cycle through a chain", want: "type composition cycle",
			src: "package t\ntable Aaa { b Bbb }\ntable Bbb { a Aaa }\n"},
		{name: "const(value, bits) in a table body", want: "const(value, bits) is a packet-wire construct",
			src: "package t\ntable Tab {\n    const(7, 4)\n    x int32\n}\n"},
		{name: "reserved(bits) in a table body", want: "reserved(bits) is a packet-wire construct",
			src: "package t\ntable Tab {\n    reserved(4)\n    x int32\n}\n"},
		{name: "align in a table body", want: "align is a packet-wire construct",
			src: "package t\ntable Tab {\n    align\n    x int32\n}\n"},
		// a TABLE arm is legal inside a table closure (docs/SPEC-TABLES.md §2.6) and
		// refused outside one, by name, from both sides: the type body that
		// holds such a union, and the union no table reaches
		{name: "a table-armed union in a type body", want: "a union in a `type` body takes `type` payloads only",
			src: "package t\ntable Tab { x int32 }\nunion U\n{\n    tab Tab\n}\ntype P { u U }\ntable Holder { u U }\n"},
		{name: "a table-armed union no table reaches", want: "no table reaches U",
			src: "package t\ntable Tab { x int32 }\nunion U\n{\n    tab Tab\n}\n"},
		{name: "a table-armed union in a type a table reaches", want: "a union in a `type` body takes `type` payloads only",
			src: "package t\ntable Tab { x int32 }\nunion U\n{\n    tab Tab\n}\ntype P { u U }\ntable Holder { p P }\n"},
		{name: "a table arm holding its own union by value is a cycle", want: "type composition cycle",
			src: "package t\ntable Tab { u U }\nunion U\n{\n    tab Tab\n}\ntable Holder { u U }\n"},
		{name: "enum headroom has no name to ride under", want: "headroom value has no NAME",
			src: "package t\nenum E | max = 8\n{ A, B }\ntable Tab { e E }\n"},
		{name: "a keyed array of unions stays a named follow-on", want: "an enum-keyed array of unions",
			src: "package t\nenum E { A, B }\ntype P { x int32 }\nunion U\n{\n    p P\n}\ntable Tab { us [E]U }\n"},
		{name: "a declaration colliding with the table runtime", want: "generated TABLE-wire runtime",
			src: "package t\ntable Tab { x int32 }\ntype TableReport { y int32 }\n"},
		{name: "a declaration colliding with generated table codecs", want: "generated TABLE-wire functions",
			src: "package t\ntable Tab { x int32 }\ntype TabLoad { y int32 }\n"},
		{name: "a declaration colliding with the mutable-life surface", want: "generated TABLE-wire functions",
			src: "package t\ntable Tab { x int32 }\ntype TabBuilder { y int32 }\n"},
		// THE PER-ENUM IDENTITY PAIR (docs/SPEC-TABLES.md §5, §11). C++ and C#
		// overload the pair on the enum's own type; JavaScript has no
		// overloading and no nested scope to hide it in, so each enum brings
		// TWO module-level bindings named after it. Claimed for every enum of
		// a unit that declares a table, whether or not any field has that type
		// — a name free today must not become a collision the day a table
		// gains one.
		{name: "a declaration colliding with an enum's table identity id", want: "identity pair",
			src: "package t\nenum Grade { A, B }\ntable Tab { g Grade }\ntype TableEnumIdGrade { y int32 }\n"},
		{name: "a declaration colliding with an enum's table identity value", want: "identity pair",
			src: "package t\nenum Grade { A, B }\ntable Tab { g Grade }\ntype TableEnumValueGrade { y int32 }\n"},
		{name: "the identity pair is claimed for an enum no table field names", want: "identity pair",
			src: "package t\nenum Grade { A, B }\ntable Tab { x int32 }\ntype TableEnumIdGrade { y int32 }\n"},
		{name: "a declaration colliding with the text form's generic walk", want: "generated TABLE-wire runtime",
			src: "package t\ntable Tab { x int32 }\ntype TableJson { y int32 }\n"},
		{name: "a declaration colliding with the block layout check", want: "generated TABLE-wire runtime",
			src: "package t\ntable Tab { x int32 }\ntype TableBlockLayout { y int32 }\n"},
		{name: "a declaration colliding with the bit helpers' scratch", want: "generated TABLE-wire runtime",
			src: "package t\ntable Tab { x int32 }\ntype TableBitsScratch { y int32 }\n"},

		// THE RUST CONSTANT SPACE (docs/SPEC-TABLES.md §11). Rust spells a
		// constant SCREAMING_SNAKE, and the spelling is MANY-TO-ONE:
		// TableCookMagic, TABLE_COOK_MAGIC and table_cook_magic all lower to
		// one crate-scope TABLE_COOK_MAGIC. Claiming the registered spelling
		// alone left the other two legal, and what they generated was a pair
		// of ambiguous glob re-exports — the crate built with a warning, the
		// author's own constant was silently shadowed at the crate root, and a
		// CONSUMER naming the symbol failed to compile under
		// ambiguous_glob_imports, which is deny-by-default and
		// future-incompatible.
		{name: "a lowercase const lowering onto a runtime constant", want: "TABLE_COOK_MAGIC",
			src: "package t\ntable Tab { x int32 }\nconst table_cook_magic = 1\n"},
		{name: "a SCREAMING const lowering onto a runtime constant", want: "TABLE_JSON_MAX_DEPTH",
			src: "package t\ntable Tab { x int32 }\nconst TABLE_JSON_MAX_DEPTH = 1\n"},
		{name: "a lowercase const lowering onto the build version", want: "BUILD_VERSION",
			src: "package t\ntable Tab { x int32 }\nconst build_version = 1\n"},
		{name: "a lowercase const lowering onto a table's block extent", want: "TAB_BLOCK_MAX_BYTES",
			src: "package t\ntable Tab { x int32 }\nconst tab_block_max_bytes = 1\n"},

		// ---- pointers (docs/SPEC-TABLES.md §11) ----
		{name: "a pointer to a type is refused by name", want: "may only target a `table`",
			src: "package t\ntype P { x int32 }\ntable Tab { p *P }\n"},
		{name: "a pointer to an enum is refused by name", want: "may only target a `table`",
			src: "package t\nenum E { A }\ntable Tab { e *E }\n"},
		{name: "a pointer inside a type body is refused by name", want: "pointers are a TABLE construct",
			src: "package t\ntable Tab { x int32 }\ntype P { t *Tab }\n"},
		{name: "a pointer field takes no specified default", want: "a pointer field takes no specified default",
			src: "package t\ntable Node { x int32 }\ntable Tab { head *Node = 0 }\n"},
		// every generated name-first spelling is claimed, including the ones a
		// value-only table never emits: adding a pointer to a table must not
		// turn a legal declaration elsewhere into a collision
		{name: "a declaration colliding with the pointer allocation entry", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeEmplace { y int32 }\n"},
		{name: "a declaration colliding with the pack walker", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodePack { y int32 }\n"},
		{name: "a declaration colliding with the pack sizer", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodePackMeasure { y int32 }\n"},
		{name: "a declaration colliding with the numbering walk", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeNumber { y int32 }\n"},
		{name: "a declaration colliding with a load's type-id dispatch", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeNodeStorage { y int32 }\n"},
		{name: "a declaration colliding with the builder load path", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeLoadBuilder { y int32 }\n"},
		{name: "a declaration colliding with the cook's read side", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeOpen { y int32 }\n"},
		{name: "a declaration colliding with the cook's write side", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeCookMeasure { y int32 }\n"},
		{name: "a declaration colliding with the descriptor storage", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeTableInfo { y int32 }\n"},
		{name: "a declaration colliding with the descriptor field storage", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeTableFields { y int32 }\n"},
		{name: "a declaration colliding with the prefill", want: "generated TABLE-wire functions",
			src: "package t\ntable Node { x int32 }\ntype NodeReset { y int32 }\n"},

		// a table named after a Builder member would emit a header that cannot
		// compile — a member function hides the type name it shares
		{name: "a table named after a builder member", want: "collides with a member of the generated",
			src: "package t\ntable Alloc { x int32 }\n"},
		{name: "a table named after the builder's lock", want: "collides with a member of the generated",
			src: "package t\ntable Lock { x int32 }\n"},
		{name: "a table named after the arena member", want: "collides with a member of the generated",
			src: "package t\ntable arena { x int32 }\n"},

		// ---- optional fields, `?T` (docs/SPEC-TABLES.md §2.3, §11) ----
		{name: "an optional in a type body is refused by name", want: "optionals are a TABLE construct",
			src: "package t\ntype P { tier ?int32 }\n"},
		{name: "an optional pointer is refused by name", want: "a pointer is ALREADY optional",
			src: "package t\ntable Node { x int32 }\ntable Tab { head ?*Node }\n"},
		// a BYTE BUFFER is a pointer, and the refusal must quote the spelling
		// the author wrote — `?*bytes`, not the bounded `?bytes(N)` they did
		// not write (docs/SPEC-TABLES.md §2.5)
		{name: "an optional byte buffer names the spelling written", want: "field b: ?*bytes marks a pointer optional",
			src: "package t\ntable Tab { b ?*bytes }\n"},
		{name: "an optional string buffer names the spelling written", want: "field s: ?*string marks a pointer optional",
			src: "package t\ntable Tab { s ?*string }\n"},
		{name: "an optional array of pointers is a named follow-on", want: "? on an array of pointers is a named follow-on",
			src: "package t\ntable Node { x int32 }\ntable Tab { xs ?[..4]*Node }\n"},
		{name: "an optional array of unions is a named follow-on", want: "? on an array of unions is a named follow-on",
			src: "package t\ntype A { x int32 }\nunion U { a A }\ntable Tab { xs ?[..4]U }\n"},
		{name: "an optional array takes no specified default", want: "an optional field takes no specified default",
			src: "package t\ntable Tab { xs ?[..4]int32 = 5 }\n"},
		{name: "an optional whose closure is variable is a named follow-on", want: "? on a value whose closure is variable-length",
			src: "package t\ntable Leaf { value int32\n    next *Leaf }\ntable Tab { opt ?Leaf }\n"},
		{name: "an optional array whose element closure is variable is a named follow-on", want: "? on a value whose closure is variable-length",
			src: "package t\ntable Leaf { value int32\n    next *Leaf }\ntable Tab { xs ?[..2]Leaf }\n"},
		{name: "an optional string is a named follow-on", want: "? on string(N) is a named follow-on",
			src: "package t\ntable Tab { s ?string(8) }\n"},
		{name: "an optional bytes is a named follow-on", want: "? on bytes(N) is a named follow-on",
			src: "package t\ntable Tab { b ?bytes(8) }\n"},
		{name: "an optional union is refused by name", want: "a union is ALREADY optional",
			src: "package t\ntype A { x int32 }\nunion U { a A }\ntable Tab { u ?U }\n"},
		{name: "an optional takes no specified default", want: "an optional field takes no specified default",
			src: "package t\ntable Tab { tier ?int32 = 5 }\n"},
		{name: "a field colliding with an optional's presence companion", want: "the generated presence companion",
			src: "package t\ntype Inner { x int32 }\ntable Tab {\n    settings ?Inner\n    settings_present bool\n}\n"},

		// ---- enum-keyed arrays, `[E]T` (docs/SPEC-TABLES.md §2.4, §11) ----
		{name: "a keyed array bound naming flags is refused by name", want: "names a `flags` declaration",
			src: "package t\nflags F { A, B }\ntable Tab { xs [F]int32 }\n"},
		{name: "a bounded enum-keyed array is refused by name", want: "a bounded enum-keyed array is refused",
			src: "package t\nenum E { A, B }\ntable Tab { xs [..E]int32 }\n"},
		{name: "a ranged enum-keyed array is refused by name", want: "a bounded enum-keyed array is refused",
			src: "package t\nenum E { A, B }\ntable Tab { xs [1..E]int32 }\n"},
		{name: "a keyed array of pointers stays a named follow-on", want: "an enum-keyed array of pointers",
			src: "package t\nenum E { A, B }\ntable Node { x int32 }\ntable Tab { kids [E]*Node }\n"},
		{name: "an optional keyed array is a named follow-on", want: "?[E]int32 is a named follow-on",
			src: "package t\nenum E { A, B }\ntable Tab { xs ?[E]int32 }\n"},
		// a BOUNDED enum-key is refused by §2.4 with the ? and without it, so
		// the keyed refusal's "drop the ?" is not true advice here — dropping
		// the ? alone lands on §2.4's refusal — and both bounded spellings
		// take the refusal that names the bound
		{name: "an optional up-to enum-keyed array names the bound", want: "a BOUNDED enum-keyed array is refused with the ? and without it",
			src: "package t\nenum E { A, B }\ntable Tab { xs ?[..E]int32 }\n"},
		{name: "an optional ranged enum-keyed array names the bound", want: "a BOUNDED enum-keyed array is refused with the ? and without it",
			src: "package t\nenum E { A, B }\ntable Tab { xs ?[A..E]int32 }\n"},
		{name: "an optional literal-ranged enum-keyed array names the bound", want: "a BOUNDED enum-keyed array is refused with the ? and without it",
			src: "package t\nenum E { A, B }\ntable Tab { xs ?[2..E]int32 }\n"},
		// a KEY is a closure vocabulary: it rides under a variant hash exactly
		// as a value does, so both §5 refusals are owed to it even when the
		// enum reaches the closure ONLY as a key
		{name: "headroom on an enum reaching a closure only as a key", want: "reserves values above the declared variants",
			src: "package t\nenum E | max = 15 { A, B }\ntable Tab { s [E]int32 }\n"},
		{name: "the key refusal names the keying field as the reaching edge", want: "field s, which keys an array by E, reaches it",
			src: "package t\nenum E | max = 15 { A, B }\ntable Tab { s [E]int32 }\n"},

		// ---- maps, `map[K]V` (docs/SPEC-TABLES.md §2.8, §11) ----
		//
		// Keys are BOUNDED STRINGS and INTEGERS, and nothing else: a key is an
		// identity, so everything below is refused because it cannot BE one.
		{name: "a map in a type body is refused by name", want: "a MAP is a table-only construct",
			src: "package t\ntype P { m map[uint32]int32 }\n"},
		// the fixture enum is NOT named E, so the row proves the diagnostic
		// SUBSTITUTES the enum the author wrote rather than printing a literal
		{name: "an enum key is refused naming [E]T", want: "an ENUM key is refused — that is [ShipType]T's job",
			src: "package t\nenum ShipType { Fighter, Bomber }\ntable Tab { m map[ShipType]int32 }\n"},
		{name: "the enum-key suggestion names the value the author wrote", want: "declare [ShipType]int32, and [ShipType]?int32",
			src: "package t\nenum ShipType { Fighter, Bomber }\ntable Tab { m map[ShipType]int32 }\n"},
		// AND IT NEVER SUGGESTS A SPELLING THIS CHECKER REFUSES: [E]*T and
		// [E]U are named follow-ons, an array of arrays has no spelling, and
		// a bound silently dropped would name a different declaration
		{name: "the enum-key suggestion keeps the value's array bound", want: "[..4]int32 is not one",
			src: "package t\nenum ShipType { Fighter, Bomber }\ntable Tab { m map[ShipType][..4]int32 }\n"},
		{name: "the enum-key suggestion never names a keyed array of pointers", want: "*V is not one",
			src: "package t\nenum ShipType { Fighter, Bomber }\ntable V { a int32 }\ntable Tab { m map[ShipType]*V }\n"},
		{name: "the enum-key suggestion never names a keyed array of unions", want: "U is not one",
			src: "package t\nenum ShipType { Fighter, Bomber }\ntype A { x int32 }\nunion U { a A }\ntable Tab { m map[ShipType]U }\n"},
		{name: "the enum-key suggestion never names a keyed array of maps", want: "map[uint8]int32 is not one",
			src: "package t\nenum ShipType { Fighter, Bomber }\ntable Tab { m map[ShipType]map[uint8]int32 }\n"},
		{name: "a bool key is refused by name", want: "a `bool` key is refused — two slots",
			src: "package t\ntable Tab { m map[bool]int32 }\n"},
		{name: "a float32 key is refused by name", want: "no total order survives a byte compare",
			src: "package t\ntable Tab { m map[float32]int32 }\n"},
		{name: "a float64 key is refused by name", want: "no total order survives a byte compare",
			src: "package t\ntable Tab { m map[float64]int32 }\n"},
		{name: "a flags key is refused by name", want: "a mask names a SET, not one thing",
			src: "package t\nflags F { A, B }\ntable Tab { m map[F]int32 }\n"},
		{name: "a bits(N) key is refused by name", want: "a bits(N) key is refused",
			src: "package t\ntable Tab { m map[bits(9)]int32 }\n"},
		{name: "a bytes(N) key is refused by name", want: "a bytes(N) key is refused",
			src: "package t\ntable Tab { m map[bytes(4)]int32 }\n"},
		{name: "a fixed-point key is refused by name", want: "a fixed-point key is refused",
			src: "package t\ntable Tab { m map[fixed(16, 16)]int32 }\n"},
		{name: "a 128-bit integer key is refused by name", want: "a 128-bit integer is not a map key",
			src: "package t\ntable Tab { m map[int128]int32 }\n"},
		{name: "a type key is refused by name", want: "a `type` key is refused",
			src: "package t\ntype P { a int32 }\ntable Tab { m map[P]int32 }\n"},
		{name: "a table key is refused by name", want: "a `table` key is refused",
			src: "package t\ntable K { a int32 }\ntable Tab { m map[K]int32 }\n"},
		{name: "a pointer key is refused by name", want: "a POINTER key is refused",
			src: "package t\ntable K { a int32 }\ntable Tab { m map[*K]int32 }\n"},
		{name: "a union key is refused by name", want: "a UNION key is refused",
			src: "package t\ntype A { x int32 }\nunion U { a A }\ntable Tab { m map[U]int32 }\n"},
		{name: "an optional key is refused by name", want: "an OPTIONAL key is refused",
			src: "package t\ntable Tab { m map[?uint32]int32 }\n"},
		// NOTHING QUALIFIES A KEY: the grammar takes the spelling so the
		// diagnostic is the page's sentence rather than a parse error
		{name: "a qualification on the key is refused by name", want: "`| max` does not apply to a map KEY",
			src: "package t\ntable Tab { m map[uint32 | max = 4]int32 }\n"},
		{name: "a min on the key is refused by name", want: "`| min` does not apply to a map KEY",
			src: "package t\ntable Tab { m map[uint32 | min = 1]int32 }\n"},
		{name: "a default on the key is refused by name", want: "a map KEY takes no default",
			src: "package t\ntable Tab { m map[uint32 = 5]int32 }\n"},
		// the map itself takes no bound, no ?, no default and no attribute: a
		// map's count is unbounded by design and its empty IS its absence
		{name: "a bounded map is refused by name", want: "a map takes no array bound",
			src: "package t\ntable Tab { m [..4]map[uint32]int32 }\n"},
		{name: "a fixed array of maps is refused by name", want: "a map takes no array bound",
			src: "package t\ntable Tab { m [4]map[uint32]int32 }\n"},
		{name: "an optional map is refused by name", want: "there is no ?map",
			src: "package t\ntable Tab { m ?map[uint32]int32 }\n"},
		{name: "a default on a map is refused by name", want: "a map takes no specified default",
			src: "package t\ntable Tab { m map[uint32]int32 = 0 }\n"},
		{name: "an attribute on a map is refused by name", want: "does not apply to a map",
			src: "package t\ntable Tab { m map[uint32]int32 | max = 4 }\n"},
		// the by-value cycle reaches THROUGH the entry, and a map of *Self is
		// the ordinary legal recursion through a pointer
		{name: "a map of ITSELF closes a by-value cycle", want: "type composition cycle",
			src: "package t\ntable Tab { m map[uint32]Tab }\n"},
		{name: "a map of a table that holds the map closes one too", want: "type composition cycle",
			src: "package t\ntable A { m map[uint32]B }\ntable B { n map[uint32]A }\n"},
		// the <Table><Field>Entry name is CLAIMED on the table that declares
		// the map, on the terms <field>_present is claimed for an optional
		{name: "a declared table under the claimed entry name is refused", want: "the entry name is CLAIMED",
			src: "package t\ntable TabShipsEntry { x int32 }\ntable Tab { ships map[uint32]int32 }\n"},
		{name: "a declared type under the claimed entry name is refused", want: "the entry name is CLAIMED",
			src: "package t\ntype TabShipsEntry { x int32 }\ntable Tab { ships map[uint32]int32 }\n"},
		// and the REST of the map surface is claimed on the declaring table
		// the same way the block form's row accessors are (§2.8, §11)
		{name: "a declaration under the claimed Find name is refused", want: "TabShipsFind",
			src: "package t\ntype TabShipsFind { x int32 }\ntable Tab { ships map[uint32]int32 }\n"},
		{name: "a declaration under the claimed IndexFind name is refused", want: "TabShipsIndexFind",
			src: "package t\nenum TabShipsIndexFind { A, B }\ntable Tab { ships map[uint32]int32 }\n"},
		// and the map's RUNTIME names are claimed as every other runtime
		// name is: whenever a unit declares a table
		{name: "a declaration named TableMap is refused", want: "TABLE-wire runtime",
			src: "package t\nenum TableMap { A, B }\ntable Tab { x int32 }\n"},
		{name: "a declaration named TableMapIndex is refused", want: "TABLE-wire runtime",
			src: "package t\nenum TableMapIndex { A, B }\ntable Tab { x int32 }\n"},

		// ---- UNBOUNDED ARRAYS (docs/SPEC-TABLES.md §2.9, §11) ----
		//
		// The element set is `[..N]T`'s exactly, so every refusal below is
		// either the construct's own placement rule or the bounded array's
		// own diagnostic met one spelling over.
		{name: "a []T in a type body is refused by name", want: "an UNBOUNDED ARRAY is a table-only construct",
			src: "package t\ntype P { xs []int32 }\n"},
		{name: "a []T as a union arm is refused by name", want: "a []T is not an arm",
			src: "package t\ntable E { a uint32 }\nunion U { xs []E }\ntable Tab { u U }\n"},
		{name: "[..]T names []T as the fix", want: "[..]T is not the unbounded array",
			src: "package t\ntable E { a uint32 }\ntable Tab { xs [..]E }\n"},
		{name: "[Min..]T names []T as the fix", want: "[Min..]T is not the unbounded array",
			src: "package t\ntable E { a uint32 }\ntable Tab { xs [0..]E }\n"},
		{name: "?[]T is refused by name", want: "there is no ?[]T",
			src: "package t\ntable E { a uint32 }\ntable Tab { xs ?[]E }\n"},
		{name: "a default on a []T is refused by name", want: "a []T takes no specified default",
			src: "package t\ntable Tab { xs []int32 = 0 }\n"},
		// the bounded spellings of the construct itself, and the element set's
		// own four refusals, each on the bounded array's own diagnostic
		{name: "[][]T is an array of arrays", want: "an array of arrays is not supported in v1",
			src: "package t\ntable Tab { xs [][]int32 }\n"},
		{name: "[..N][]T is an array of arrays", want: "an array of arrays is not supported in v1",
			src: "package t\ntable Tab { xs [..4][]int32 }\n"},
		{name: "[N][]T is an array of arrays", want: "an array of arrays is not supported in v1",
			src: "package t\ntable Tab { xs [4][]int32 }\n"},
		{name: "[]map takes the map's own bound refusal", want: "a map takes no array bound",
			src: "package t\ntable Tab { xs []map[uint32]int32 }\n"},
		{name: "[]*bytes takes the byte buffer's own array refusal", want: "an array of byte buffers is a named follow-on",
			src: "package t\ntable Tab { xs []*bytes }\n"},
		// a table that holds a [] of ITSELF by value closes a by-value cycle,
		// and a []*Self is the ordinary legal recursion through a pointer
		{name: "a [] of ITSELF closes a by-value cycle", want: "type composition cycle",
			src: "package t\ntable Tab { xs []Tab }\n"},
		{name: "a [] of a table that holds one closes a by-value cycle too", want: "type composition cycle",
			src: "package t\ntable A { xs []B }\ntable B { ys []A }\n"},
		// AN UNBOUNDED ARRAY CLAIMS THREE NAMES against its field, and the
		// difference from the map's eight is the key on both sides
		{name: "a declaration under the claimed Add name is refused", want: "TabXsAdd",
			src: "package t\ntable TabXsAdd { a int32 }\ntable Tab { xs []int32 }\n"},
		{name: "a declaration under the claimed Each name is refused", want: "TabXsEach",
			src: "package t\ntype TabXsEach { a int32 }\ntable Tab { xs []int32 }\n"},
		{name: "a declaration under the claimed Erase name is refused", want: "TabXsErase",
			src: "package t\nenum TabXsErase { A, B }\ntable Tab { xs []int32 }\n"},
		{name: "a declaration named TableList is refused", want: "TABLE-wire runtime",
			src: "package t\nenum TableList { A, B }\ntable Tab { x int32 }\n"},

		{name: "a pointer to an undeclared table", want: "undefined type",
			src: "package t\ntable Tab { head *Ghost }\n"},
		{name: "by-value recursion stays refused with pointers in the language",
			want: "type composition cycle",
			src:  "package t\ntable Node { x int32\n  self Node }\n"},
		{name: "by-value recursion through a chain stays refused",
			want: "type composition cycle",
			src:  "package t\ntable A { b B }\ntable B { a A }\n"},

		// the Dart backend's table verbs are MEMBERS of the value, so a field
		// whose Dart spelling is one of them is refused on every closure member
		{name: "a table field spelled as a Dart table verb", want: "collides with the generated Dart table verb `measure`",
			src: "package t\ntable Tab { measure int32 }\n"},
		{name: "a type a table reaches, with a field spelled as a Dart table verb", want: "collides with the generated Dart table verb `toJson`",
			src: "package t\ntype P { to_json int32 }\ntable Tab { p P }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
			if len(errs) == 0 {
				t.Fatalf("expected a diagnostic containing %q, got none", tc.want)
			}
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
}

// TestDartVerbClaimNeedsATable: the Dart member-verb claim is a table-wire
// claim, and a table-free unit keeps `measure`, `save` and the rest for its
// own fields.
func TestDartVerbClaimNeedsATable(t *testing.T) {
	buildUnit(t, "package t\ntype P { measure int32\n  save int32\n  to_json int32 }\n")
}

// TestTypeFreeOfTableSymbols: a table-free unit keeps its whole namespace —
// the TableReport claim exists only when a table is declared.
func TestTypeFreeOfTableSymbols(t *testing.T) {
	if errs := runUnit(t, map[string]string{"T.schema": "package t\ntype TableReport { y int32 }\n"}); len(errs) > 0 {
		t.Fatalf("a table-free unit must not claim the table runtime names: %v", errs)
	}
	// and the RUST CONSTANT SPACE is scoped the same way: a table-free unit
	// keeps every spelling that would lower onto a runtime constant
	for _, name := range []string{"table_cook_magic", "TABLE_JSON_MAX_DEPTH", "build_version", "tab_block_max_bytes"} {
		src := "package t\nconst " + name + " = 1\ntype P { x int32 }\n"
		if errs := runUnit(t, map[string]string{"T.schema": src}); len(errs) > 0 {
			t.Errorf("a table-free unit must keep %s: %v", name, errs)
		}
	}
}

// TestRustConstantSpaceIsClaimedForEveryRuntimeConstant is the SELF-MAINTAINING
// half: every registered name the Rust backend spells as a crate-scope constant
// is refused in the MAPPED space, whatever spelling the schema uses to reach
// it. A registry entry somebody adds without the mapped claim fails here.
//
// It walks the registry rather than a list of its own, so the two cannot
// disagree — the same reason internal/check reads tablenames rather than
// keeping a second copy.
func TestRustConstantSpaceIsClaimedForEveryRuntimeConstant(t *testing.T) {
	names := tablenames.RustConstants()
	if len(names) == 0 {
		t.Fatal("the registry names no Rust crate-scope constant at all — the registry, not the claim, is what broke")
	}
	for _, name := range names {
		lowered := strings.ToLower(ir.RustConstName(name))
		src := "package t\ntable Tab { x int32 }\nconst " + lowered + " = 1\n"
		errs := runUnit(t, map[string]string{"T.schema": src})
		if len(errs) == 0 {
			t.Errorf("a declaration named %s was accepted beside a table — it lowers to the runtime's own %s, "+
				"and the generated crate would carry two glob re-exports of that symbol; internal/check must "+
				"claim the Rust constant space for every tablenames.RustConstants() entry",
				lowered, ir.RustConstName(name))
			continue
		}
		named := false
		for _, e := range errs {
			if strings.Contains(e.Error(), ir.RustConstName(name)) {
				named = true
			}
		}
		if !named {
			t.Errorf("%s is refused, but the diagnostic does not name the symbol %s it collides with: %v",
				lowered, ir.RustConstName(name), errs)
		}
	}
}

const tablelessSrc = `package probe

const MaxItems = 16

enum Kind { Alpha, Beta }

type Point
{
    x float32
    y float32
}

type Packet
{
    kind  Kind
    items [..MaxItems]Point
}
`

// TestKeyedSpellingIsTheSameTypeWire: on the TYPE wire `[E]T` IS `[E.Max]T` —
// positional, bitpacked, one slot per NAMED VARIANT and nothing for None. The
// two spellings must project identically and carry one protocol id, or a keyed
// array would be a packet-wire edit dressed as a table-wire feature.
func TestKeyedSpellingIsTheSameTypeWire(t *testing.T) {
	const keyed = `package probe

enum Kind { Alpha, Beta }

type Board
{
    per_kind [Kind]int32
}
`
	const spelled = `package probe

enum Kind { Alpha, Beta }

type Board
{
    per_kind [Kind.Max]int32
}
`
	a := buildUnit(t, keyed)
	b := buildUnit(t, spelled)
	if ir.WireProjection(a) != ir.WireProjection(b) {
		t.Fatalf("[E]T and [E.Max]T project differently:\n--- [E]T ---\n%s\n--- [E.Max + 1]T ---\n%s",
			ir.WireProjection(a), ir.WireProjection(b))
	}
	if a.ProtocolId != b.ProtocolId {
		t.Fatalf("[E]T and [E.Max]T carry different protocol ids %#x != %#x", a.ProtocolId, b.ProtocolId)
	}
	// and the resolved bound is the slot count: one per named variant, and
	// nothing for None (docs/SPEC-TABLES.md §2.4)
	f := a.Structs["Board"].Fields[0]
	if f.Array != ir.ArrayFixed || f.ArrayBound != 2 || f.KeyEnum != "Kind" {
		t.Fatalf("keyed field resolved wrong: array=%v bound=%d key=%q", f.Array, f.ArrayBound, f.KeyEnum)
	}
}

// TestTablesMoveNoProtocolId is the independence requirement: packets and
// tables version independently, so ADDING a table (and everything in its
// closure that was already there) moves neither the projection nor the id —
// and neither does EDITING one.
func TestTablesMoveNoProtocolId(t *testing.T) {
	withTable := tablelessSrc + `
table Config
{
    scale  float32 = 1.0
    points [..8]Point
}
`
	editedTable := tablelessSrc + `
table Config
{
    scale   float32 = 2.0
    points  [..8]Point
    comment string(32)
}
`
	// the table-body constructs are table-wire only: an optional field and an
	// enum-keyed array move no packet byte either
	keyedTable := tablelessSrc + `
table Config
{
    scale   float32 = 2.0
    points  [..8]Point
    comment string(32)
    slots   [Kind]Point
    origin  ?Point
}
`
	base := buildUnit(t, tablelessSrc)
	added := buildUnit(t, withTable)
	edited := buildUnit(t, editedTable)
	keyed := buildUnit(t, keyedTable)

	if ir.WireProjection(base) != ir.WireProjection(added) {
		t.Fatalf("adding a table changed the wire projection:\n--- without ---\n%s\n--- with ---\n%s",
			ir.WireProjection(base), ir.WireProjection(added))
	}
	if base.ProtocolId != added.ProtocolId {
		t.Fatalf("adding a table moved the protocol id %#x -> %#x", base.ProtocolId, added.ProtocolId)
	}
	if added.ProtocolId != edited.ProtocolId {
		t.Fatalf("editing a table moved the protocol id %#x -> %#x", added.ProtocolId, edited.ProtocolId)
	}
	if ir.WireProjection(base) != ir.WireProjection(keyed) {
		t.Fatalf("an optional field or a keyed array changed the wire projection:\n--- without ---\n%s\n--- with ---\n%s",
			ir.WireProjection(base), ir.WireProjection(keyed))
	}
	if base.ProtocolId != keyed.ProtocolId {
		t.Fatalf("an optional field or a keyed array moved the protocol id %#x -> %#x", base.ProtocolId, keyed.ProtocolId)
	}
}

// TestJsonKeyMovesNoWire is §16.3's independence requirement: the text form's
// key is the TEXT's vocabulary and the wire id is the WIRE's, so declaring a
// json key moves no byte on either wire — not a table field's id, not the
// packet projection, not the protocol id. Keys are the text's business.
func TestJsonKeyMovesNoWire(t *testing.T) {
	plain := tablelessSrc + `
table Config
{
    ship_type int32
    label     string(32)
}
type Keyed
{
    weight float32
}
`
	keyed := tablelessSrc + `
table Config
{
    ship_type int32      | json = "type"
    label     string(32) | json = "name"
}
type Keyed
{
    weight float32
}
`
	before := buildUnit(t, plain)
	after := buildUnit(t, keyed)

	if ir.WireProjection(before) != ir.WireProjection(after) {
		t.Fatalf("a json key changed the wire projection:\n--- without ---\n%s\n--- with ---\n%s",
			ir.WireProjection(before), ir.WireProjection(after))
	}
	if before.ProtocolId != after.ProtocolId {
		t.Fatalf("a json key moved the protocol id %#x -> %#x", before.ProtocolId, after.ProtocolId)
	}
	for i, f := range after.Tables["Config"].Fields {
		was := before.Tables["Config"].Fields[i]
		if ir.TableFieldId(f) != ir.TableFieldId(was) {
			t.Errorf("field %s: a json key moved the table-wire id %#04x -> %#04x",
				f.Name, ir.TableFieldId(was), ir.TableFieldId(f))
		}
		if ir.TableFieldId(f) != ir.FieldId(f.Name) {
			t.Errorf("field %s: the wire id is not the hash of the SCHEMA name", f.Name)
		}
	}
	// and the key itself is what the walk reads and writes under
	if got := ir.TableFieldJsonKey(after.Tables["Config"].Fields[0]); got != "type" {
		t.Errorf("json key = %q, want %q", got, "type")
	}
	// a field with no attribute keys under its own name
	if got := ir.TableFieldJsonKey(before.Tables["Config"].Fields[0]); got != "ship_type" {
		t.Errorf("default json key = %q, want the field name", got)
	}
}

// TestTableFieldIds pins the id function and the was override.
func TestTableFieldIds(t *testing.T) {
	// frozen values: the id is WIRE FORMAT — a change here breaks every
	// stored table on earth
	pins := map[string]uint16{
		"velocity": 0x2e46,
		"damage":   0x15a9,
		"name":     0x30df,
	}
	for name, want := range pins {
		if got := ir.FieldId(name); got != want {
			t.Errorf("FieldId(%q) = %#04x, want %#04x — the table-wire id function moved", name, got, want)
		}
	}

	u := buildUnit(t, "package t\ntable Tab { speed float32 | was = \"velocity\" }\n")
	f := u.Tables["Tab"].Fields[0]
	if got := ir.TableFieldId(f); got != ir.FieldId("velocity") {
		t.Errorf("was alias not honored: id %#04x, want hash of the old name %#04x", got, ir.FieldId("velocity"))
	}
	if ir.TableFieldId(f) == ir.FieldId("speed") {
		t.Error("was alias ignored — the renamed field kept its new-name id")
	}
}

// TestTablesInvisibleToPacketIR: tables live beside the decl stream, never in
// it — Structs, File.Decls and the projection must not see them.
func TestTablesInvisibleToPacketIR(t *testing.T) {
	u := buildUnit(t, tablelessSrc+"\ntable Config { scale float32 = 1.0 }\n")
	if _, leaked := u.Structs["Config"]; leaked {
		t.Fatal("a table leaked into Unit.Structs — the packet backends would emit it")
	}
	if u.Tables["Config"] == nil {
		t.Fatal("the table is missing from Unit.Tables")
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && st.IsTable {
				t.Fatal("a table leaked into File.Decls — the packet backends would emit it")
			}
		}
	}
	if strings.Contains(ir.WireProjection(u), "Config") {
		t.Fatal("a table leaked into the wire projection — the protocol id depends on it")
	}
}

// TestTableModeDerivation: the mode is DERIVED, never declared. Fixed-size is
// the pointer-free by-value closure — a plain struct, exactly as every table
// was before pointers existed. Variable-length is a pointer anywhere in that
// closure, and the mode propagates UP through by-value nesting only
// (docs/SPEC-TABLES.md §2).
func TestTableModeDerivation(t *testing.T) {
	u := buildUnit(t, `package t

type Plain
{
    x int32
}

table Leaf
{
    v int32
}

table Node
{
    v    int32
    next *Node
}

table HoldsPointer
{
    head *Leaf
}

table NestsVariable
{
    inner Node
}

table ArrayOfVariable
{
    kids [..4]Node
}

table StaysFixed
{
    leaf  Leaf
    p     Plain
    items [..4]int32
}
`)
	variable := ir.VariableTables(u)
	for _, name := range []string{"Node", "HoldsPointer", "NestsVariable", "ArrayOfVariable"} {
		if !variable[name] {
			t.Errorf("%s should derive VARIABLE-LENGTH (a pointer in its by-value closure)", name)
		}
	}
	for _, name := range []string{"Leaf", "StaysFixed", "Plain"} {
		if variable[name] {
			t.Errorf("%s should stay FIXED-SIZE — no pointer in its by-value closure, so it pays for none of the machinery", name)
		}
	}

	// pointing AT a variable table does not make the pointer's owner's
	// NESTER variable through the pointer edge — only the declaring table
	targets := ir.PointerTargets(u)
	if !targets["Node"] || !targets["Leaf"] {
		t.Errorf("pointer targets missing: %v", targets)
	}
	if targets["StaysFixed"] {
		t.Error("StaysFixed is nobody's pointer target")
	}
}

// TestPointerRecursionIsLegal: recursion through a POINTER edge is the whole
// point of the freedom tables were given — the by-value cycle refusal must
// exempt it (docs/SPEC-TABLES.md §2).
func TestPointerRecursionIsLegal(t *testing.T) {
	u := buildUnit(t, "package t\ntable Node\n{\n    v    int32\n    next *Node\n}\n")
	f := u.Tables["Node"].Fields[1]
	if !f.Type.Pointer {
		t.Fatal("the *Node field did not resolve as a pointer")
	}
	if f.Type.Ref == nil || f.Type.Name != "Node" {
		t.Fatalf("the pointer's target did not resolve: %+v", f.Type)
	}
}

// TestCanonicalRootTableNameIsLegal: `table Root` is this spec's own canonical
// example, and the expressiveness gate (§12) is written around a root table.
// The generated builder's accessors are GetRoot/AsConst precisely so that the
// name stays available — a member function would otherwise hide the type name
// it shares and the header would not compile.
func TestCanonicalRootTableNameIsLegal(t *testing.T) {
	u := buildUnit(t, "package t\ntable Node\n{\n    v    int32\n    next *Node\n}\n\ntable Root\n{\n    head *Node\n}\n")
	if u.Tables["Root"] == nil {
		t.Fatal("table Root did not survive checking")
	}
	if !ir.VariableTables(u)["Root"] {
		t.Error("table Root did not derive VARIABLE-LENGTH")
	}
}

// TestRetiredOpenWalkIsFree: `<X>OpenWalk` named wire v1's validating walk, and
// §7's Open is a header match with NO WALK IN IT — so the name went with the
// design and the claim went with the name (docs/SPEC-TABLES.md §7, §11). A claim
// nothing needs takes a name away from every schema for free, and this is the
// one direction the claim list cannot police itself in: an unclaimed name is
// invisible unless something asks for it.
func TestRetiredOpenWalkIsFree(t *testing.T) {
	u := buildUnit(t, "package t\ntable Node { x int32 }\ntype NodeOpenWalk { y int32 }\n")
	if u.Structs["NodeOpenWalk"] == nil {
		t.Fatal("type NodeOpenWalk did not survive checking: the retired claim is still being made")
	}
}

// TestTableWideExtents: the table wire carries u32 lengths and u32 counts, so
// an extent past 65535 is ordinary — the ceiling is the language's own int32
// storage cap, not a wire ceiling (docs/SPEC-TABLES.md §2.2, §3).
func TestTableWideExtents(t *testing.T) {
	cases := map[string]string{
		"a string past 65535":       "package t\ntable Tab { s string(70000) }\n",
		"bytes past 65535":          "package t\ntable Tab { b bytes(100000) }\n",
		"an array count past 65535": "package t\ntable Tab { xs [..70000]int32 }\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if errs := runUnit(t, map[string]string{"T.schema": src}); len(errs) > 0 {
				t.Fatalf("an extent past 65535 must be legal on the table wire: %v", errs)
			}
		})
	}
}

// TestTableVariantIds pins the variant/arm id function: variants ride under
// the SAME fold field names take, so one implementation serves both.
func TestTableVariantIds(t *testing.T) {
	for _, name := range []string{"Gold", "buff", "Silver"} {
		if ir.VariantId(name) != ir.FieldId(name) {
			t.Errorf("VariantId(%q) diverged from the field-id fold", name)
		}
	}
	// frozen: the variant id is WIRE FORMAT
	pins := map[string]uint16{"Bronze": 0x3407, "Silver": 0xa3e7, "Gold": 0xda27}
	for name, want := range pins {
		if got := ir.VariantId(name); got != want {
			t.Errorf("VariantId(%q) = %#04x, want %#04x — the table-wire variant id function moved", name, got, want)
		}
	}
}

// TestTableFileDag is docs/SPEC-TABLES.md §11's cross-file DAG rule, held in the
// FRONT END so every target refuses the same units. C++ makes the consequence
// concrete — the generated <A>Table.h and <B>Table.h would have to include
// each other — but a unit legal under one target and illegal under another is
// the trap the rule exists to prevent, so the check does not live in a
// backend. Nothing downstream can disagree with it: no Generator runs until
// check.Unit succeeds.
func TestTableFileDag(t *testing.T) {
	t.Run("a two-file cycle is refused, naming the closing declaration", func(t *testing.T) {
		errs := runUnit(t, map[string]string{
			"A.schema": "package p\ntable AlphaHolder { b Beta }\ntable Alpha { v int32 }\n",
			"B.schema": "package p\ntable Beta { a Alpha }\n",
		})
		if len(errs) == 0 {
			t.Fatal("a cross-file table reference cycle was accepted")
		}
		for _, e := range errs {
			if strings.Contains(e.Error(), "cross-file table reference cycle") &&
				strings.Contains(e.Error(), "Beta closes") {
				return
			}
		}
		t.Fatalf("no diagnostic names the cycle and its closing declaration: %v", errs)
	})

	t.Run("a cycle closed through a union arm is refused", func(t *testing.T) {
		errs := runUnit(t, map[string]string{
			"A.schema": "package p\ntable Holder { b Beta }\ntype Leaf { v int32 }\n",
			"B.schema": "package p\nunion Pick\n{\n    leaf Leaf\n}\ntable Beta { p Pick }\n",
		})
		if len(errs) == 0 {
			t.Fatal("a cycle closed through a union arm was accepted")
		}
		for _, e := range errs {
			if strings.Contains(e.Error(), "cross-file table reference cycle") {
				return
			}
		}
		t.Fatalf("no diagnostic names the cycle: %v", errs)
	})

	// the controls: only edges that LEAVE a file are graphed, so an acyclic
	// cross-file graph and same-file pointer recursion both stay legal
	t.Run("an acyclic cross-file graph is legal", func(t *testing.T) {
		if errs := runUnit(t, map[string]string{
			"A.schema": "package p\ntable Holder { b Beta }\n",
			"B.schema": "package p\ntable Beta { v int32 }\n",
		}); len(errs) > 0 {
			t.Fatalf("an acyclic cross-file table graph must be legal: %v", errs)
		}
	})

	t.Run("same-file recursion through a pointer stays legal", func(t *testing.T) {
		if errs := runUnit(t, map[string]string{
			"A.schema": "package p\ntable Node { v int32\n  next *Node }\n",
		}); len(errs) > 0 {
			t.Fatalf("same-file pointer recursion must stay legal: %v", errs)
		}
	})

	t.Run("a table-free unit is not graphed at all", func(t *testing.T) {
		if errs := runUnit(t, map[string]string{
			"A.schema": "package p\ntype Holder { b Beta }\n",
			"B.schema": "package p\ntype Beta { v int32 }\n",
		}); len(errs) > 0 {
			t.Fatalf("a table-free unit must not be graphed: %v", errs)
		}
	})
}

// TestSpecSection11EqualsTheChecker holds docs/SPEC-TABLES.md §11's suffix
// lists to `tableGeneratedVerbs`, in both directions and spelling for
// spelling.
//
// THE PAGE CLAIMS THIS OF ITSELF — "the three lists here are
// `tableGeneratedVerbs` entire" — and a claim a page makes about the code is
// exactly the claim that rots quietly. It did: a blind reader counted 33
// spellings on the page against the checker's 40, because the C backend's
// seven were added to the checker and never written down. A name the page
// states and the checker does not claim is a name a user may take and then
// find their generated code will not compile; a name the checker claims and
// the page does not state is a name taken from every schema with no notice.
//
// The page is parsed rather than restated here: the three fenced blocks are
// its own text, so a list edited on one side and not the other fails.
func TestSpecSection11EqualsTheChecker(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "SPEC-TABLES.md"))
	if err != nil {
		t.Fatalf("SPEC-TABLES.md: %v", err)
	}
	// the three suffix blocks: the base set, the block form's, and the C
	// backend's. Each opens with a line naming its first spelling, which is
	// what anchors the scan to the right fences.
	stated := map[string]bool{}
	lines := strings.Split(string(page), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "Measure  MeasureBody  Save  SaveBody  SaveBodyFields  Load  LoadBody" &&
			trimmed != "Block  BlockStorage  BlockBegin  BlockBytes  BlockMaxBytes  BlockOpen" &&
			trimmed != "BuilderInit  BuilderShutdown  BuilderLock  BuilderRoot" {
			continue
		}
		for _, body := range lines[i:] {
			if strings.Contains(body, "```") {
				break
			}
			for word := range strings.FieldsSeq(body) {
				stated[word] = true
			}
		}
	}
	if len(stated) == 0 {
		t.Fatal("§11's suffix blocks were not found in the page at all — the scan, not the page, is what broke")
	}

	held := map[string]bool{}
	for _, verb := range tableGeneratedVerbs {
		held[verb] = true
	}
	for verb := range held {
		if !stated[verb] {
			t.Errorf("the checker claims the suffix %q and §11 does not state it — a name taken from every "+
				"schema with no notice on the page; add it to the section's lists", verb)
		}
	}
	for verb := range stated {
		if !held[verb] {
			t.Errorf("§11 states the suffix %q and the checker does not claim it — a user may declare that "+
				"name and find the generated code will not compile; add it to tableGeneratedVerbs", verb)
		}
	}
	if len(stated) != len(held) {
		t.Errorf("§11 states %d suffixes and the checker holds %d", len(stated), len(held))
	}
}

// TestTableArmsProjectAndTablesDoNot is §2.6's rule beside §10's boundary. A
// UNION is a declaration like any other and every arm projects as the field
// line it is, so adding an arm moves the protocol id exactly as adding a field
// does. A TABLE's own fields never project, so the independence that matters
// holds: no table edit reaches this id.
func TestTableArmsProjectAndTablesDoNot(t *testing.T) {
	withTables := tablelessSrc + `
table Open { path string(16) }
table Save { path string(16) }
union Body
{
    open Open
}
table Msg { body Body }
`
	moreArms := tablelessSrc + `
table Open { path string(16) }
table Save { path string(16) }
union Body
{
    open Open
    save Save
}
table Msg { body Body }
`
	grownTable := strings.Replace(moreArms, "table Open { path string(16) }", "table Open {\n    path string(16)\n    line uint32\n}", 1)
	// §20.8's ARM id control: a union a table closure holds gains a SCALAR
	// arm, and the id moves — an arm is a field line whatever its type
	scalarArm := strings.Replace(moreArms, "    save Save\n", "    save Save\n    seq  uint32\n", 1)

	base := buildUnit(t, tablelessSrc)
	one := buildUnit(t, withTables)
	two := buildUnit(t, moreArms)
	grown := buildUnit(t, grownTable)
	scalar := buildUnit(t, scalarArm)
	if base.ProtocolId == one.ProtocolId {
		t.Fatalf("declaring a union did NOT move the protocol id %#x — a union is a declaration and its arms are wire facts", base.ProtocolId)
	}
	if one.ProtocolId == two.ProtocolId {
		t.Fatalf("adding an arm did NOT move the protocol id %#x — an arm projects as the field line it is", one.ProtocolId)
	}
	if two.ProtocolId == scalar.ProtocolId {
		t.Fatalf("adding a SCALAR arm did NOT move the protocol id %#x — an arm is a field line whatever its type (§20.8)", two.ProtocolId)
	}
	// the independence that holds: a TABLE's own fields project nothing, so
	// growing Open moves no packet byte and no packet id (§10)
	if two.ProtocolId != grown.ProtocolId {
		t.Fatalf("a table's own field moved the protocol id %#x -> %#x — no table edit may reach this id", two.ProtocolId, grown.ProtocolId)
	}
	un := two.TableUnions["Body"]
	if un == nil || !un.HasTableArm() || len(un.Variants) != 2 || !un.Variants[1].Ref.IsTable {
		t.Fatalf("Body did not resolve its table arms: %+v", un)
	}
}

// TestTableArmModeRunsThroughArms is §2.2's rule for §2.6: a union of FIXED
// table arms leaves its holder fixed, and one VARIABLE arm makes it variable.
func TestTableArmModeRunsThroughArms(t *testing.T) {
	fixed := buildUnit(t, `package t
table Open { path string(16) }
union Body
{
    open Open
}
table Msg { body Body }
`)
	if v := ir.VariableTables(fixed); v["Msg"] {
		t.Fatalf("a union of fixed table arms made its holder variable: %v", v)
	}
	variable := buildUnit(t, `package t
table Chunk { next *Chunk }
table Open { path string(16) }
union Body
{
    open  Open
    chunk Chunk
}
table Msg { body Body }
`)
	if v := ir.VariableTables(variable); !v["Msg"] || !v["Chunk"] || v["Open"] {
		t.Fatalf("a variable arm did not run the mode through the union: %v", v)
	}
}

// TestEveryScalarRidesInATable is the negative control of the refusal §11 no
// longer states: every scalar the TYPE wire carries is legal in a table body
// and in a type a table reaches, and each lands on the kind docs/SPEC-TABLES.md
// §3 gives it — the 128-bit integers under their own two kinds, and the
// fixed-point family under one kind per storage width and signedness, the raw
// scaled integer riding at the storage width.
func TestEveryScalarRidesInATable(t *testing.T) {
	u := buildUnit(t, "package t\n"+
		"type Inner\n{\n    q fixed(2, 30) = 1.0 | min = -1, max = 1\n}\n"+
		"table Tab\n{\n"+
		"    a fixed(4, 4)      | min = -8, max = 7\n"+
		"    b fixed(16, 16)    | min = -180, max = 180\n"+
		"    c fixed(16, 48)    | min = -1000, max = 1000\n"+
		"    d fixed(112, 16)   | min = -1000000, max = 1000000\n"+
		"    e ufixed(4, 4)     | min = 0, max = 15\n"+
		"    f ufixed(16, 16)   | min = 0, max = 360\n"+
		"    g ufixed(48, 16)   | min = 0, max = 60000\n"+
		"    h ufixed(64, 64)   | min = 0, max = 1000\n"+
		"    i int128           | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376\n"+
		"    j uint128\n"+
		"    k [3]fixed(8, 8)   | min = -1, max = 1\n"+
		"    l [..2]uint128\n"+
		"    m fixed(32, 32) = 2.5 | min = -4, max = 4\n"+
		"    n ufixed(64, 0)    | min = 0, max = 9223372036854775807\n"+
		"    inner Inner\n"+
		"}\n")
	want := map[string]int{
		"a": ir.TableKindFixed8, "b": ir.TableKindFixed32, "c": ir.TableKindFixed64, "d": ir.TableKindFixed128,
		"e": ir.TableKindUFixed8, "f": ir.TableKindUFixed32, "g": ir.TableKindUFixed64, "h": ir.TableKindUFixed128,
		"i": ir.TableKindI128, "j": ir.TableKindU128, "k": ir.TableKindFixed16, "l": ir.TableKindU128,
		"m": ir.TableKindFixed64, "n": ir.TableKindUFixed64,
	}
	for _, f := range u.Tables["Tab"].Fields {
		if k, ok := want[f.Name]; ok && ir.TableScalarKind(f) != k {
			t.Errorf("field %s rides under kind %d, the page gives it %d", f.Name, ir.TableScalarKind(f), k)
		}
		if ir.TableKindWide(ir.TableScalarKind(f)) && ir.TableKindWidth(ir.TableScalarKind(f))*8 != f.Type.Width {
			t.Errorf("field %s: kind width %d bytes against a %d-bit storage", f.Name, ir.TableKindWidth(ir.TableScalarKind(f)), f.Type.Width)
		}
	}
	lo, hi, ok := ir.TableRawRange(u.Tables["Tab"].Fields[1])
	if !ok || lo.String() != "-11796480" || hi.String() != "11796480" {
		t.Errorf("fixed(16, 16) [-180, 180] should clamp raw at ±11796480, got %v %v %v", lo, hi, ok)
	}
	if got := ir.TableWideFields(u); len(got) != 15 {
		t.Errorf("TableWideFields names %d fields, the unit declares 15: %v", len(got), got)
	}
	if u.Structs["Inner"].Fields[0].DefInt.String() != "1073741824" {
		t.Errorf("the fixed default is held RAW: %v", u.Structs["Inner"].Fields[0].DefInt)
	}
}

// TestPointerArraysAreLegal is §2.1's bounded spellings: `[N]*T` and `[..N]*T`
// are pointer slots per element, they make their holder variable-length, and
// their element rides under kind 17.
func TestPointerArraysAreLegal(t *testing.T) {
	u := buildUnit(t, `package t
table Node { x int32 }
table Tab
{
    kids  [..4]*Node
    pair  [2]*Node
}
`)
	tab := u.Tables["Tab"]
	if tab == nil || len(tab.Fields) != 2 {
		t.Fatalf("Tab did not resolve: %+v", tab)
	}
	for _, f := range tab.Fields {
		if !f.Type.Pointer || f.Array == ir.ArrayNone {
			t.Fatalf("%s: not an array of pointers: %+v", f.Name, f.Type)
		}
		if ir.TableFieldKind(f) != ir.TableKindArray || ir.TableElemKind(f) != ir.TableKindPointer {
			t.Fatalf("%s: kinds %d/%d, want 14/17", f.Name, ir.TableFieldKind(f), ir.TableElemKind(f))
		}
	}
	if v := ir.VariableTables(u); !v["Tab"] || v["Node"] {
		t.Fatalf("an array of pointers did not make its holder variable: %v", v)
	}
	if pieces := ir.FieldPieces(u, tab.Fields[1], 0); len(pieces) != 1 || pieces[0].Size != 16 {
		t.Fatalf("[2]*Node is two eight-byte slots, got %+v", pieces)
	}
}

// TestUnionArraysAreLegal is §2.6's bounded spellings: `[N]U` and `[..N]U`
// are a union slot per element under kind 14 with element kind 15, a holder
// of fixed arms stays fixed, a VARIABLE arm makes the holder variable through
// the array exactly as through a field, and the enum-keyed spelling `[E]U`
// stays a named follow-on.
func TestUnionArraysAreLegal(t *testing.T) {
	u := buildUnit(t, `package t
type P { x int32 }
table Node { y int32 }
union U
{
    p P
    n Node
}
table Tab
{
    log  [..4]U
    undo [2]U
}
table Var { next *Node }
union V
{
    step Var
}
table Holder { history [..2]V }
`)
	tab := u.Tables["Tab"]
	if tab == nil || len(tab.Fields) != 2 {
		t.Fatalf("Tab did not resolve: %+v", tab)
	}
	for _, f := range tab.Fields {
		if f.Array == ir.ArrayNone {
			t.Fatalf("%s: not an array: %+v", f.Name, f.Type)
		}
		if ir.TableFieldKind(f) != ir.TableKindArray || ir.TableElemKind(f) != ir.TableKindUnion {
			t.Fatalf("%s: kinds %d/%d, want 14/15", f.Name, ir.TableFieldKind(f), ir.TableElemKind(f))
		}
	}
	if got := ir.TableUnionArrays(u); len(got) != 3 {
		t.Fatalf("TableUnionArrays names %d fields, the unit declares 3: %v", len(got), got)
	}
	v := ir.VariableTables(u)
	if v["Tab"] {
		t.Fatalf("an array of unions over fixed arms made its holder variable: %v", v)
	}
	if !v["Holder"] {
		t.Fatalf("a VARIABLE arm did not make the holder variable through the array: %v", v)
	}
}

// THE THREE RESERVED-ID REFUSALS, AND THE COLLISION REFUSALS BESIDE THEM
// (docs/SPEC-TABLES.md §3, §3.3, §5, §11). No declarable name hashes to either
// id the language holds back — the node table's 0xFFFFFFFFFFFFFFFF or the
// announcement's build version 0xFFFFFFFFFFFFFFFE — or to another name's id at
// sixty-four bits, so each control
// PLANTS THE COLLISION BELOW THE HASH — the compiler's test hook returns the
// colliding value for one named spelling and nothing for any other. Without the
// hook there is no input that could turn these refusals red, and a refusal no
// input can reach is a refusal nothing watches.
func TestTheIdRefusalsUnderAPlantedCollision(t *testing.T) {
	const reserved = uint64(0xFFFFFFFFFFFFFFFF)
	// the second id the language holds back: the ANNOUNCEMENT's build version
	// (docs/SPEC-TABLES.md §3.3), refused on exactly the node table's terms
	const reservedBuildVersion = uint64(0xFFFFFFFFFFFFFFFE)
	cases := []struct {
		name  string
		plant map[string]uint64
		src   string
		want  string
	}{
		{
			name:  "a field name that takes the reserved node-table id",
			plant: map[string]uint64{"head": reserved},
			src:   "package t\ntable Tab { head int32 }\n",
			want:  "takes one of the two ids the language holds back",
		},
		{
			name:  "a `was` alias that takes the reserved node-table id",
			plant: map[string]uint64{"old_head": reserved},
			src:   "package t\ntable Tab { head int32 | was = \"old_head\" }\n",
			want:  "takes one of the two ids the language holds back",
		},
		{
			name:  "a table name that takes the reserved node-table id",
			plant: map[string]uint64{"Tab": reserved},
			src:   "package t\ntable Tab { x int32 }\n",
			want:  "takes one of the two ids the language holds back",
		},
		{
			name:  "a field name that takes the reserved build-version id",
			plant: map[string]uint64{"head": reservedBuildVersion},
			src:   "package t\ntable Tab { head int32 }\n",
			want:  "takes one of the two ids the language holds back",
		},
		{
			name:  "a `was` alias that takes the reserved build-version id",
			plant: map[string]uint64{"old_head": reservedBuildVersion},
			src:   "package t\ntable Tab { head int32 | was = \"old_head\" }\n",
			want:  "takes one of the two ids the language holds back",
		},
		{
			name:  "a table name that takes the reserved build-version id",
			plant: map[string]uint64{"Tab": reservedBuildVersion},
			src:   "package t\ntable Tab { x int32 }\n",
			want:  "takes one of the two ids the language holds back",
		},
		{
			name:  "two tables of one closure colliding on the type id",
			plant: map[string]uint64{"Tab": 0x0123456789abcdef, "Other": 0x0123456789abcdef},
			src:   "package t\ntable Tab { x int32 }\ntable Other { y int32 }\n",
			want:  "collide on table-wire type id",
		},
		{
			name:  "two fields of one table colliding on the wire id",
			plant: map[string]uint64{"a": 0x0123456789abcdef, "b": 0x0123456789abcdef},
			src:   "package t\ntable Tab {\n    a int32\n    b int32\n}\n",
			want:  "collide on table-wire id",
		},
		{
			name:  "two enum variants colliding on the wire id",
			plant: map[string]uint64{"A": 0x0123456789abcdef, "B": 0x0123456789abcdef},
			src:   "package t\nenum E { A, B }\ntable Tab { e E }\n",
			want:  "collide on table-wire id",
		},
		{
			name:  "two union arms colliding on the wire id",
			plant: map[string]uint64{"one": 0x0123456789abcdef, "two": 0x0123456789abcdef},
			src:   "package t\nunion U {\n    one int32\n    two float32\n}\ntable Tab { u U }\n",
			want:  "collide on table-wire id",
		},
		{
			name:  "an enum a closure reaches only as an array KEY",
			plant: map[string]uint64{"A": 0x0123456789abcdef, "B": 0x0123456789abcdef},
			src:   "package t\nenum E { A, B }\ntable Tab { s [E]int32 }\n",
			want:  "collide on table-wire id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plant := tc.plant
			ir.TableWireIdHook = func(name string) (uint64, bool) {
				v, ok := plant[name]
				return v, ok
			}
			t.Cleanup(func() { ir.TableWireIdHook = nil })
			errs := runUnit(t, map[string]string{"T.schema": tc.src})
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("no diagnostic contains %q; got: %v", tc.want, errs)
		})
	}
	// the NEGATIVE CONTROL of the hook itself: with nothing planted, the same
	// sources compile, so what the rows above show is the refusal and not the
	// source
	for _, tc := range cases {
		errs := runUnit(t, map[string]string{"T.schema": tc.src})
		for _, e := range errs {
			if strings.Contains(e.Error(), tc.want) {
				t.Fatalf("%s: the refusal fired with nothing planted — the row proves the source, not the check", tc.name)
			}
		}
	}
}

// TestListQualificationBoundsTheElement: a bar attribute on a `[]T` qualifies
// the ELEMENT, exactly as it does on a `[..N]T` (docs/SPEC-TABLES.md §2.9,
// §11). The construct has no spelling for a count bound, and `max` is not
// one: it is the element's range, and the checker judges it on the bounded
// array's own terms.
func TestListQualificationBoundsTheElement(t *testing.T) {
	u := buildUnit(t, "package t\ntable Tab { xs []int32 | min = 0, max = 4 }\n")
	f := u.Tables["Tab"].Fields[0]
	if !f.IsList() {
		t.Fatalf("xs is not an unbounded array: %+v", f.Array)
	}
	if !f.HasIntRange || f.IntMin.Int64() != 0 || f.IntMax.Int64() != 4 {
		t.Fatalf("the qualification did not bound the element: has=%v min=%v max=%v", f.HasIntRange, f.IntMin, f.IntMax)
	}
}
