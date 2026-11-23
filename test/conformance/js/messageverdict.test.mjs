// The message-load verdict the JavaScript wire-fuzz adapters answer
// (messageverdict.mjs), replayed over the committed cases
// (testdata/message-verdict/cases.json). Run by `make js-message-verdict-test`,
// which test-js depends on. No generated code, no files written, no time.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { messageLoadVerdict } from "./messageverdict.mjs";

const corpus = JSON.parse(readFileSync(new URL("./testdata/message-verdict/cases.json", import.meta.url), "utf8"));

const verdictOf = (v) => (v.delivered ? "delivered" : v.refused ? "refused" : v.malformed ? "malformed" : "none");

test("the corpus is whole", () => {
  assert.ok(Array.isArray(corpus.cases) && corpus.cases.length > 0, "the corpus names no case");
  const ids = new Set();
  for (const c of corpus.cases) {
    assert.ok(!ids.has(c.id), "the case " + c.id + " is named twice");
    ids.add(c.id);
    assert.ok(["delivered", "refused", "malformed"].includes(c.verdict), c.id + ": no verdict");
  }
  for (const id of ["delivered", "delivered-oor", "delivered-mismatch", "refused-verdict", "damaged"]) {
    assert.ok(ids.has(id), "the corpus has lost the case " + id);
  }
});

for (const c of corpus.cases) {
  test(c.id, () => {
    const v = messageLoadVerdict(c.n, c.report, corpus.refusedVerdict);
    assert.equal(verdictOf(v), c.verdict, c.what);
    // exactly one of the three
    assert.equal([v.delivered, v.refused, v.malformed].filter(Boolean).length, 1, c.id + ": " + JSON.stringify(v));
  });
}

// NEGATIVE CONTROL: the predicate before F-112 (a counter above zero turned a
// delivered batch into a refusal) fails the F-112 cases and passes every
// control, so the corpus tells the two apart.
test("negative control: the counter predicate fails the F-112 cases", () => {
  const before = (n, rep, refusedVerdict) => {
    const refused = rep.Verdict === refusedVerdict || Boolean(rep.NonFiniteFloat) || ((rep.Mismatch | 0) + (rep.Oor | 0) > 0 && !rep.Damaged);
    const malformed = Boolean(rep.Damaged) || (n < 1 && !refused);
    return { refused, malformed, delivered: n >= 1 && !malformed && !refused };
  };
  const failed = corpus.cases.filter((c) => verdictOf(before(c.n, c.report, corpus.refusedVerdict)) !== c.verdict).map((c) => c.id);
  const f112 = corpus.cases.filter((c) => c.what.startsWith("F-112")).map((c) => c.id);
  assert.ok(f112.length > 0, "the corpus carries no F-112 case");
  assert.deepEqual(failed, f112);
});
