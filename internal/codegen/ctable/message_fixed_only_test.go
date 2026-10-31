package ctable

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Only a fixed table rides the message form (docs/SPEC-TABLES.md §3.3). A
// variable root has no message verb and no refusal named for one, and a
// struct or a union only variable tables hold has no message codec; a fixed
// root's batch API and its named retention refusals are emitted.
func TestMessageFormFixedTablesOnly(t *testing.T) {
	u := cUnitOf(t, `package probe
type Inner { x int32 }
type Onlyvar { y int32 }
union Held { inner Inner }
union Varchoice { only Onlyvar }
fixed table Fixedroot { a uint32
inner Inner
held Held }
table Varleaf { n int32 }
table Varroot { leaf *Varleaf
only Onlyvar
choice Varchoice }
table Varplain { v int32 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	text := all.String()
	for _, want := range []string{
		"fixedroot_load_messages(", "fixedroot_load_messages_tolerant(", "fixedroot_save_messages(", "fixedroot_measure_messages(",
		"inner_save_message_body(", "schema_probe_held_wire_message_save_(",
		"#define fixedroot_load_retain_messages(", "#define fixedroot_save_retain_messages(",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the fixed root's message surface lacks %q", want)
		}
	}
	// Onlyvar is a struct and not a root: it keeps the named retention
	// refusals every non-variable member carries, and loses its message codecs.
	absent := regexp.MustCompile(`\b(varroot|varplain|varleaf)_[a-z_]*messages?\b|\bonlyvar_(load|save|measure|load_measure)_messages\b|\b(varroot|varplain|varleaf|onlyvar)_[a-z_]*message_body\b|\bschema_probe_(varroot|varplain|varleaf|onlyvar)_message_|\bschema_probe_varchoice_wire_message_|\btable_retain_message_|\btable_message_nodes_open\b`)
	if hits := absent.FindAllString(text, -1); len(hits) > 0 {
		t.Fatalf("a variable table carries message-form code: %q", hits)
	}
}
