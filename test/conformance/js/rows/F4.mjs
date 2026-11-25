// F4 — layout_malformed, truncated (docs/FIXED-FORM-ALGORITHM.md §2 step 3).
//
// A fixed-form file is: the 16-byte header (form byte 3, seven reserved zero
// bytes, the layout hash at 8), then the u32 LAYOUT LENGTH L at 16, then L
// layout bytes, then the records. The layout therefore ends at `20 + L`.
// Step 3 says: `L := LE(4, b+16)`; if `20 + L > bytes`, REFUSE layout_malformed.
//
// That is the TRUNCATED case, and it is a REFUSAL BY NAME (report.refused =
// TableFixedRefusal.LayoutMalformed) and never the `malformed` FLAG. The flag
// is F7, "malformed, under 20 bytes" — a file shorter than the header — which
// is the residue and not a name. The two are distinct: this row proves the
// NAME, fired when the file reaches the header but is cut inside the layout.
//
// THE CHECK RUNS BEFORE THE HASH SELECT, so the header's hash is irrelevant to
// it: a truncated layout is refused layout_malformed even for a hash in no
// lineage, before layout_unknown or no_layout could have their say.
//
// Reaches the generated tree exactly as the driver does (test/conformance/js/
// main.mjs): the same SCHEMA_JS_GENERATED indirection and the same module
// resolve, so a negative control that points the same path at a sabotaged copy
// of the generated modules makes this row red.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const tables = await load("examples/TablesTable.js");
const home = await load("examples/TabledemoTable.js");

let failed = false;
function check(ok, what) {
  console.log((ok ? "ok  " : "FAIL") + " " + what);
  if (!ok) { failed = true; }
}

// The production entrypoint: WeaponConfigFixedLoad, the emitted reader. Its
// call site is this file and the law it exercises is the truncation guard in
// fixedmodule.go (`if (layoutBytes + layoutAt > byteLength)`), emitted into
// every FixedLoad. Body 22 bytes, record 30, layout 191 — the smallest fixed
// root in the tables corpus, so the numbers below are its own constants.
const HEADER = home.TableFixedHeaderBytes;          // 16
const LENGTH_AT = home.TableFixedHeaderBytes;       // the u32 L at offset 16
const LAYOUT_AT = HEADER + home.TableFixedLayoutHeaderBytes; // 20
const L = tables.WeaponConfigFixedLayoutBytes;      // the layout's own length

const one = [new home.WeaponConfig()];
const file = new Uint8Array(tables.WeaponConfigFixedMeasure(1));
check(tables.WeaponConfigFixedSave(one, 1, file) === file.length,
  "positive: a lawful file saves (length " + file.length + " = 20 + " + L + " + record 30)");

{
  const back = [new home.WeaponConfig()];
  const rep = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(back, back.length, file, file.length, tables.WeaponConfigFixedNewPlan(), rep);
  check(n === 1 && rep.refused === 0 && !("damaged" in rep),
    "positive: the whole file reads clean (n=" + n + ", refused=" + rep.refused + ")");
}

// THE TRUNCATED LAYOUT: cut the file one byte into the layout, so 20 + L > bytes.
{
  const cut = file.subarray(0, LAYOUT_AT + L - 1);
  const back = [new home.WeaponConfig()];
  const rep = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(back, back.length, cut, cut.length, tables.WeaponConfigFixedNewPlan(), rep);
  check(n === -1 && rep.refused === home.TableFixedRefusal.LayoutMalformed && !("damaged" in rep),
    "F4: a layout cut to 20+L-1 bytes is layout_malformed, the NAME not the flag (n=" + n + ", refused=" + rep.refused + ")");
}

// THE SAME NAME FOR ANY OVER-RUN LENGTH, using a minimal vector from the law:
// the check is `20 + L > bytes`, so a file of exactly 20 bytes that claims any
// nonzero L is truncated, no matter what the hash says.
{
  const v = new Uint8Array(20);
  v[0] = home.TableFixedForm;
  new DataView(v.buffer).setUint32(LENGTH_AT, 1, true); // L = 1, so 20 + 1 > 20
  const back = [new home.WeaponConfig()];
  const rep = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(back, back.length, v, v.length, tables.WeaponConfigFixedNewPlan(), rep);
  check(n === -1 && rep.refused === home.TableFixedRefusal.LayoutMalformed && !("damaged" in rep),
    "F4: a 20-byte header claiming L=1 is layout_malformed before the hash select (refused=" + rep.refused + ")");
}

// THE BOUNDARY: `20 + L == bytes` is NOT truncated at step 3. Cut the file
// exactly at the end of the layout (drop the records), so 20 + L == bytes and
// rest == 0: the reader proceeds PAST step 3's guard. What it meets next is
// step 9 (docs/FIXED-FORM-ALGORITHM.md §5.3, line ~1201): "if rest == 0 ...
// REFUSE truncated — A FIXED-FORM FILE HOLDS AT LEAST ONE RECORD". This row
// once read that file as zero records and no refusal; the at-least-one-record
// rule (§5.3 step 9, SPEC-TABLES §22.6) made it a refusal named `truncated`
// (§5.3's refusal table; "There is no `malformed` flag on this form's
// report"), never the layout_malformed NAME. The boundary this row pins is the step-3 guard, so
// the assertion that bites is "not layout_malformed".
{
  const exact = file.subarray(0, LAYOUT_AT + L);
  const back = [new home.WeaponConfig()];
  const rep = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(back, back.length, exact, exact.length, tables.WeaponConfigFixedNewPlan(), rep);
  check(n === -1 && rep.refused !== home.TableFixedRefusal.LayoutMalformed,
    "F4 boundary: exactly 20+L bytes passes step 3 — never layout_malformed (n=" + n + ", refused=" + rep.refused + ")");
  check(rep.refused === home.TableFixedRefusal.Truncated && !("damaged" in rep),
    "F4 boundary: a header and a layout with no record behind it refuses truncated — a fixed-form file holds at least one record (refused=" + rep.refused + ")");
}

// PROOF THE READER GOT PAST STEP 3 ON THE BOUNDARY: the same exact cut under a
// hash in no lineage entry is answered by STEP 5 (layout_unknown), which only
// a reader that passed step 3 can reach. One byte shorter, the same file is
// step 3's layout_malformed whatever the hash says (the case above).
{
  const exact = new Uint8Array(file.subarray(0, LAYOUT_AT + L));
  new DataView(exact.buffer).setUint32(home.TableFixedHashAt, 0xdeadbeef, true);
  const back = [new home.WeaponConfig()];
  const rep = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(back, back.length, exact, exact.length, tables.WeaponConfigFixedNewPlan(), rep);
  check(n === -1 && rep.refused === home.TableFixedRefusal.LayoutUnknown,
    "F4 boundary: at exactly 20+L bytes a stranger's hash reaches step 5's layout_unknown (refused=" + rep.refused + ")");
}

// ONE RECORD PAST THE BOUNDARY reads: 20 + L + one record is the smallest
// lawful file (the positive control above is exactly this file).
{
  check(file.length === LAYOUT_AT + L + tables.WeaponConfigFixedRecordBytes,
    "F4 boundary: the lawful file is exactly 20 + L + one " + tables.WeaponConfigFixedRecordBytes + "-byte record");
}

// THE DISTINCTION FROM F7: a file UNDER the 20-byte header refuses `truncated`,
// never the layout_malformed NAME. The guard this row pins is the NAME,
// so a short header must NOT come back refused=LayoutMalformed.
{
  const short = file.subarray(0, 19);
  const back = [new home.WeaponConfig()];
  const rep = new home.TableFixedReport();
  const n = tables.WeaponConfigFixedLoad(back, back.length, short, short.length, tables.WeaponConfigFixedNewPlan(), rep);
  check(n === -1 && rep.refused === home.TableFixedRefusal.Truncated && !("damaged" in rep),
    "F4 vs F7: a 19-byte file refuses truncated, not layout_malformed (refused=" + rep.refused + ")");
}

if (failed) {
  console.log("FAILED");
  process.exit(1);
}
console.log("F4 OK");
