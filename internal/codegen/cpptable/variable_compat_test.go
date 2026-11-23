package cpptable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/baseline"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Form 1 has no writer declaration: an older required or present optional
// field may ride at the value a plain reader now elides, and the writer's
// field order may differ from the reader's. The old and new sources are each
// parsed and checked, and their own generated C++ produces both wires.
func TestCppVariableWriterEvolution(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, oldFields, newFields, oldSet, newSet, check string }{
		{"required_removed", "a int32 | required\n c int32", "a int32\n c int32", "value.c = 7;", "value.c = 7;", "value->a == 0 && value->c == 7"},
		{"optional_removed", "b ?int32\n c int32", "b int32\n c int32", "value.b_present = true; value.c = 7;", "value.c = 7;", "value->b == 0 && value->c == 7"},
		{"reordered", "a int32\n b int32", "b int32\n a int32", "value.a = 1; value.b = 2;", "value.a = 1; value.b = 2;", "value->a == 1 && value->b == 2"},
		{"widened_optional", "b ?int16\n c int32", "b int32\n c int32", "value.b_present = true; value.c = 7;", "value.c = 7;", "value->b == 0 && value->c == 7"},
		{"optional_shapes", "word string(8) | required\n data bytes(4) | required\n items ?[2]int32\n counts ?[..2]int32\n child ?Child\n mark ?Mode\n c int32", "word string(8)\n data bytes(4)\n items [2]int32\n counts [..2]int32\n child Child\n mark Mode\n c int32", "value.items_present = true; value.counts_present = true; value.child_present = true; value.mark_present = true; value.c = 7;", "value.c = 7;", "value->word_length == 0 && value->data_length == 0 && value->items[0] == 0 && value->items[1] == 0 && value->counts_count == 0 && value->child.n == 0 && value->mark == Mode::None && value->c == 7"},
		{"required_pointer", "p *Child | required\n c int32", "p *Child\n c int32", "value.c = 7;", "value.c = 7;", "value->p.null() && value->c == 7"},
		{"required_union", "pick Choice | required\n c int32", "pick Choice\n c int32", "value.c = 7;", "value.c = 7;", "value->pick.type == ChoiceType::None && value->c == 7"},
		{"required_list", "values []int32 | required\n c int32", "values []int32\n c int32", "value.c = 7;", "value.c = 7;", "value->values.count == 0 && value->c == 7"},
		{"required_map", "values map[uint32]int32 | required\n c int32", "values map[uint32]int32\n c int32", "value.c = 7;", "value.c = 7;", "value->values.count == 0 && value->c == 7"},
		{"required_keyed", "values [Mode]int32 | required\n c int32", "values [Mode]int32\n c int32", "value.c = 7;", "value.c = 7;", "value->c == 7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			schema := func(fields string) string {
				return "package compat\n enum Mode { One, Two }\n type Leaf { n int32 }\n union Choice { leaf Leaf }\n table Child { n int32 }\n table Root {\n" + fields + "\n}\n"
			}
			old := cppUnitFrom(t, schema(c.oldFields))
			current := cppUnitFrom(t, schema(c.newFields))
			refusals, _ := baseline.Split(baseline.Diff(baseline.Render(old), baseline.Render(current), baseline.DefaultTokenPolicy))
			if strings.Contains(c.oldFields, "?") {
				// `?T` to `T` is the narrowing the baseline refuses (R16), and
				// it is the ONLY refusal: `widened_optional`'s int16 to int32
				// is a widening and passes (R17). The decoder still reads the
				// old optional writer, and that read is what the case checks.
				if len(refusals) == 0 {
					t.Fatalf("`?T` to `T` must be refused by the baseline (R16)")
				}
				for _, r := range refusals {
					if !strings.Contains(r.What, "presence removed") {
						t.Fatalf("a refusal other than the optional narrowing: %v", refusals)
					}
				}
			} else if len(refusals) != 0 {
				t.Fatalf("the baseline refuses the proposed edit: %v", refusals)
			}
			write := func(u *ir.Unit, set string) []byte {
				t.Helper()
				body := fmt.Sprintf(`Root value; RootReset(value); %s
uint8_t wire[4096]; int64_t n = RootSave(&value, wire, sizeof(wire));
if (n <= 0) { return 1; } for (int64_t i = 0; i < n; i++) { printf("%%02x", wire[i]); }
`, set)
				out, err := cppHeldRun(t, u, body)
				if err != nil {
					t.Fatalf("writer: %v\n%s", err, out)
				}
				wire, err := hex.DecodeString(strings.TrimSpace(string(out)))
				if err != nil || len(wire) == 0 {
					t.Fatalf("writer wire %q: %v", out, err)
				}
				return wire
			}
			wire := write(old, c.oldSet)
			canonical := write(current, c.newSet)
			body := fmt.Sprintf(`const uint8_t wire[] = { %s };
const uint8_t want[] = { %s };
const Root *value = RootReadStrict(wire, sizeof(wire), NULL);
if (value == NULL) { printf("old wire refused\n"); return 1; }
if (!(%s)) { printf("decoded value wrong\n"); return 2; }
uint8_t saved[4096]; int64_t n = RootSave(value, saved, sizeof(saved));
if (n != (int64_t)sizeof(want) || memcmp(saved, want, sizeof(want)) != 0) { printf("new canonical save differs\n"); return 3; }
table_release(TableDefaultAllocator(), (void *) value);
if (RootReadStrict(wire, sizeof(wire)-1, NULL) != NULL) { printf("truncated wire accepted\n"); return 4; }
`, cppBytes(wire), cppBytes(canonical), c.check)
			out, err := cppHeldRun(t, current, body)
			if err != nil {
				t.Fatalf("reader: %v\n%s", err, out)
			}
		})
	}
}

// Enum variants may be reordered without changing the names on keyed form-1
// slots. Each schema is independently checked and the baseline must admit the
// edit; the two generated writers establish the old input and new rewrite.
func TestCppVariableKeyedOrderEvolution(t *testing.T) {
	t.Parallel()
	schema := func(variants string) string {
		return "package compat\n enum Mode { " + variants + " }\n table Root {\n values [Mode]int32\n}\n"
	}
	old := cppUnitFrom(t, schema("One, Two"))
	current := cppUnitFrom(t, schema("Two, One"))
	refusals, _ := baseline.Split(baseline.Diff(baseline.Render(old), baseline.Render(current), baseline.DefaultTokenPolicy))
	if len(refusals) != 0 {
		t.Fatalf("the baseline refuses enum reorder: %v", refusals)
	}
	write := func(u *ir.Unit) []byte {
		t.Helper()
		out, err := cppHeldRun(t, u, `Root value; RootReset(value);
value.values[Mode::One] = 5; value.values[Mode::Two] = 7;
uint8_t wire[4096]; int64_t n = RootSave(&value, wire, sizeof(wire));
if (n <= 0) { return 1; } for (int64_t i = 0; i < n; i++) { printf("%02x", wire[i]); }
`)
		if err != nil {
			t.Fatalf("writer: %v\n%s", err, out)
		}
		wire, err := hex.DecodeString(strings.TrimSpace(string(out)))
		if err != nil || len(wire) == 0 {
			t.Fatalf("writer wire %q: %v", out, err)
		}
		return wire
	}
	wire, canonical := write(old), write(current)
	body := fmt.Sprintf(`const uint8_t wire[] = { %s }; const uint8_t want[] = { %s };
const Root *value = RootReadStrict(wire, sizeof(wire), NULL);
if (!value) { printf("old keyed order refused\n"); return 1; }
if (value->values[Mode::One] != 5 || value->values[Mode::Two] != 7) { printf("keyed values moved\n"); return 2; }
uint8_t saved[4096]; int64_t n = RootSave(value, saved, sizeof(saved));
if (n != (int64_t)sizeof(want) || memcmp(saved, want, sizeof(want)) != 0) { printf("keyed canonical save differs\n"); return 3; }
table_release(TableDefaultAllocator(), (void *) value);
if (RootReadStrict(wire, sizeof(wire)-1, NULL)) { printf("truncated keyed wire accepted\n"); return 4; }
`, cppBytes(wire), cppBytes(canonical))
	out, err := cppHeldRun(t, current, body)
	if err != nil {
		t.Fatalf("reader: %v\n%s", err, out)
	}
}
