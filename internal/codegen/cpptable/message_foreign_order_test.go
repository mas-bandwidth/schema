package cpptable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// A different announcement may describe a writer whose fields or enum slots
// have a different order. Both sources are checked and their actual C++
// writers produce the announcement, message and reader's canonical rewrite.
func TestCppMessageForeignWriterOrder(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, old, current, set, check string }{
		{"field_order", "fixed table Root {\n a int32\n b int32\n}", "fixed table Root {\n b int32\n a int32\n}", "value.a = 1; value.b = 2;", "value.a == 1 && value.b == 2"},
		{"keyed_order", "enum Mode { One, Two }\n fixed table Root {\n values [Mode]int32\n}", "enum Mode { Two, One }\n fixed table Root {\n values [Mode]int32\n}", "value.values[Mode::One] = 5; value.values[Mode::Two] = 7;", "value.values[Mode::One] == 5 && value.values[Mode::Two] == 7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			old := cppUnitFrom(t, "package compat\n"+c.old)
			current := cppUnitFrom(t, "package compat\n"+c.current)
			if ir.BuildVersion(old) == ir.BuildVersion(current) {
				t.Fatal("the old and new writers have the same build version")
			}
			write := func(u *ir.Unit) ([]byte, []byte) {
				t.Helper()
				out, err := cppHeldRun(t, u, fmt.Sprintf(`uint8_t ann[4096]; const int64_t an = Announce(ann, sizeof(ann));
Root value; RootReset(value); %s
uint8_t wire[4096]; TableReport rep; const int64_t n = RootSaveMessages(&value, 1, wire, sizeof(wire), &rep);
if (an <= 0 || n <= 0 || rep.damaged || rep.refused) { return 1; }
for (int64_t i=0; i<an; i++) { printf("%%02x", ann[i]); } printf("\n");
for (int64_t i=0; i<n; i++) { printf("%%02x", wire[i]); } printf("\n");
`, c.set))
				if err != nil {
					t.Fatalf("writer: %v\n%s", err, out)
				}
				lines := strings.Fields(string(out))
				if len(lines) != 2 {
					t.Fatalf("writer output: %q", out)
				}
				ann, e1 := hex.DecodeString(lines[0])
				wire, e2 := hex.DecodeString(lines[1])
				if e1 != nil || e2 != nil {
					t.Fatalf("writer hex: %v %v", e1, e2)
				}
				return ann, wire
			}
			ann, wire := write(old)
			_, canonical := write(current)
			body := fmt.Sprintf(`const uint8_t ann[] = { %s }; const uint8_t wire[] = { %s }; const uint8_t want[] = { %s };
TableMessageEntry entries[kTableMessageEntriesHere + 16]; TableVocabulary vocabulary(entries, kTableMessageEntriesHere + 16); TableReport ar;
if (!AnnounceRead(vocabulary, ann, sizeof(ann), &ar) || vocabulary.build_version == BuildVersion) { printf("foreign announcement refused\n"); return 1; }
Root value; int64_t count = 1; TableReport rep;
if (!RootLoadMessages(&value, &count, vocabulary, wire, sizeof(wire), &rep) || count != 1 || rep.damaged || rep.refused) { printf("foreign order refused\n"); return 2; }
if (!(%s)) { printf("foreign values moved\n"); return 3; }
uint8_t saved[4096]; TableReport sr; const int64_t n = RootSaveMessages(&value, 1, saved, sizeof(saved), &sr);
if (n != (int64_t)sizeof(want) || memcmp(saved, want, sizeof(want)) != 0) { printf("message canonical save differs\n"); return 4; }
`, cppBytes(ann), cppBytes(wire), cppBytes(canonical), c.check)
			out, err := cppHeldRun(t, current, body)
			if err != nil {
				t.Fatalf("reader: %v\n%s", err, out)
			}
		})
	}
}
