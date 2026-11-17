package jstable

import "testing"

// Stored text rejects NUL before a bound can hide it, and a skipped value and
// every object key are held to the same rule (docs/SPEC-TABLES.md §4, §16.3).
// Other control escapes remain valid.
func TestStoredTextZeroRefusalJavaScript(t *testing.T) {
	u := loadSchemaString(t, "text.schema", `package textprobe
table Child { name string(2) }
table Root {
 name string(2)
 blob *string
 children []Child
 names map[string(2)]int32
 ids map[int32]int32
 objects map[string(2)]Child
}
`)
	jsDiscardRun(t, u, nil, `
const bad = [
 ['{"name":"ab\\u0000"}', "Root.name"],
 ['{"blob":"\\u0000"}', "Root.blob"],
 ['{"children":[{"name":"ab\\u0000"}]}', "Child.name"],
 ['{"names":{"long\\u0000":1}}', "Root.names"],
 ['{"ids":{"long\\u0000":1}}', "Root.ids"],
 ['{"objects":{"a":{"name":"ok"},"b\\u0000":{}}}', "Root.objects"],
];
for (const [text, field] of bad) {
 const r = new m.TableVariableReport();
 r.unknown = 3;
 const value = m.RootFromJson(text, r);
 if (value !== null || !r.damaged || r.reason !== "text_holds_zero: " + field ||
     r.oor !== 0 || r.unknown !== 3) {
  fail("stored NUL must refuse before clamping: " + text + " " + JSON.stringify(r));
 }
 if (m.RootFromJson('{"name":"ok"}', r) === null || r.reason || r.refused)
  fail("prior text refusal poisoned a clean read");
}
for (const text of ['{"unknown":"\\u0000"}','{"\\u0000":"ignored"}']) {
 const r = new m.TableVariableReport();
 if (m.RootFromJson(text, r) !== null || !r.damaged)
  fail("skipped NUL must refuse: " + text);
}
for (const text of ['{"name":"\\u0001"}','{"name":"ab"}']) {
 const r = new m.TableVariableReport();
 if (m.RootFromJson(text, r) === null || r.damaged || r.reason)
  fail("valid/skipped control text refused: " + text);
}
`)
}
