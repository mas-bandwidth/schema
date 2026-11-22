// THE VERDICT OF A MESSAGE LOAD, as the JavaScript wire-fuzz adapters answer
// it (main.mjs, forms 2 and 5). Conformance compares the verdict, read or
// refused, and never a reason or a counter (docs/rulings.json:
// refusal-verdict-only, conformance-is-read-then-write). So the adapter's
// verdict is the public API's own: what <Root>LoadMessages returned and the
// verdict its report names. A batch the public API delivered is delivered,
// whatever its Mismatch, Oor or Widened counters say.
//
// F-112: the adapters also called a delivered batch refused when Mismatch or
// Oor was above zero. The public API returned count 1 with item 50, and the
// adapter answered loaded 0, refused 1. That is a verdict the reader never
// gave, and it hid the readers' own false accepts (F-105, F-110) from every
// run that expected a refusal.
//
// n is what LoadMessages returned; rep is the report it filled; refusedVerdict
// is the runtime's TableMessageOpenRefused.
export function messageLoadVerdict(n, rep, refusedVerdict) {
  // a named verdict, or a non-finite float (which LoadMessages answers with
  // -1 and its own refusal), is a refusal
  const refused = rep.Verdict === refusedVerdict || Boolean(rep.NonFiniteFloat);
  // a batch that delivers no body and names no refusal is damage
  const malformed = Boolean(rep.Damaged) || (n < 1 && !refused);
  const delivered = n >= 1 && !malformed && !refused;
  return { refused, malformed, delivered };
}
