// THE DART LEG'S EMITTER-SIDE CONFORMANCE CONTROL (docs/PORTING.md I11): the
// generated FromJsonFields walk stores a bool field's value as read. Store its
// negation instead, in the emitter, so every regenerated table with a bool
// field reads `true` as `false`. The writer never runs this arm, so
// `dart / json-read` goes red while `json-write` and `wire` stay green, which
// is what says the harness found the defect in GENERATED code and localized it
// to the reader.
package main

func init() {
	sabotages["table-dart-text-walk-bool"] = []edit{{
		old: `		g.pf("\t\t\t\tif (val is bool) { v.%s = val; } else { r.kindMismatch++; }\n", name)`,
		new: `		g.pf("\t\t\t\tif (val is bool) { v.%s = !val; /* SABOTAGED: the read stores the negation */ } else { r.kindMismatch++; }\n", name)`,
	}}
}

// THE DART LEG'S ZERO RULE CONTROL (docs/SPEC-TABLES.md §16.3, text_holds_zero):
// take the refusal out of _jsonText, the one helper every text-storage read
// goes through, so a decoded U+0000 is stored as text again. Exactly the two
// carried hostile rows (str-zero-escape-value, str-zero-escape-map-key) go red;
// the skipped unknown-key zero and every other surface stay green.
func init() {
	sabotages["table-dart-text-zero"] = []edit{{
		old: "  if (s.contains('\\u0000')) throw const FormatException('text_holds_zero (docs/SPEC-TABLES.md §16.3)');\n  return s;",
		new: "  return s; // SABOTAGED: text_holds_zero removed",
	}}
}
