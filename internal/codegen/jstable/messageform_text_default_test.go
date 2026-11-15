package jstable

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestMessageStringDefaultElidedOnlyWhenEqual holds the message form's text
// elision to the reference (docs/SPEC-TABLES.md §3.3): a string or bytes
// field is left out only when its length AND its bytes are the declared
// default's. A value of the default's length ("xyz" under "abc") and the
// empty value both ride and round-trip unchanged; the default itself is
// still elided, so its bytes never reach the wire.
func TestMessageStringDefaultElidedOnlyWhenEqual(t *testing.T) {
	t.Parallel()

	node := jsNode(t)
	u := loadSchemaString(t, "message_text_default.schema", `package messagetextdefault

fixed table Probe
{
    s string(8) = "abc"
    b bytes(4) = "ab"
}
`)
	st := findTable(t, u, "Probe")
	probe := messageProbeHeader(u, st, "")
	probe += `
const enc = new TextEncoder();
function body(s, b) {
  const p = new Probe();
  p.s.fill(0); p.s.set(enc.encode(s)); p.sLength = s.length;
  p.b.fill(0); p.b.set(enc.encode(b)); p.bLength = b.length;
  return p;
}
function text(p) {
  return JSON.stringify([Array.from(p.s.subarray(0, p.sLength)), Array.from(p.b.subarray(0, p.bLength))]);
}
const bodies = [body("xyz", "zz"), body("", ""), body("abc", "ab")];
const buf = new Uint8Array(4096);
const saveReport = {};
const n = ProbeSaveMessages(bodies, buf, saveReport);
if (n < 0) { fail("SaveMessages: " + JSON.stringify(saveReport)); }
const batch = buf.subarray(0, Math.max(n, 0));
const hex = Buffer.from(batch).toString("hex");
if (!hex.includes(Buffer.from("xyz").toString("hex"))) { fail("the batch does not carry \"xyz\": " + hex); }
if (hex.includes(Buffer.from("abc").toString("hex"))) { fail("the default \"abc\" was written, not elided: " + hex); }

const back = Array.from({ length: 3 }, () => new Probe());
const report = {};
const count = ProbeLoadMessages(back, v, batch, report);
if (count !== 3) { fail("LoadMessages: count=" + count + " " + JSON.stringify(report)); }
for (let i = 0; i < 3; i++) {
  if (text(back[i]) !== text(bodies[i])) { fail("body " + i + " did not round-trip: got " + text(back[i]) + " want " + text(bodies[i])); }
}
`
	probe += probeTail()
	out, err := jsRunMessageProbe(t, node, u, map[string][]byte{
		"ann.bin": tablewire.Announce(u),
	}, probe)
	if err != nil {
		t.Fatalf("message text default: %v\n%s", err, out)
	}
}
