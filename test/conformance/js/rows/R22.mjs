// test/conformance/js/rows/R22.mjs — the closure rule for the JavaScript leg.
//
// GUARDS the closure rule (docs/FIXED-FORM-ALGORITHM.md §5.5; the same law in
// docs/SPEC-TABLES.md §21.4 and §2.2):
//
//     closure(T) is T, every table or type it reaches by value, every enum,
//     union, flags and constant any of them names, recursively. Every table or
//     type in it is declared fixed; a pointer, map or unbounded array in it is
//     a compile refusal; T is never in its own closure.
//
// THE PRODUCTION PATH. The compiler emits the closure itself, into the fixed
// table's generated module: internal/codegen/jstable/fixedform.go writes
// `<Table>FixedLayout` — a Uint8Array of `u32 count` then `count` 17-byte
// entries (id u64, kind u8, size u32, children u32), a PRE-ORDER walk of
// closure(T) in declared order (docs/FIXED-FORM-ALGORITHM.md §1). This test
// imports that generated module the way the driver does — SCHEMA_JS_GENERATED,
// default build/tables-generated-js — and holds the emitted layout to the three
// clauses of the rule that are observable at run time.
//
// WHAT IS OBSERVABLE OF EACH CLAUSE IN THE EMITTED LAYOUT:
//   1. "a pointer, map or unbounded array in the closure is a compile refusal":
//      of the three, only a POINTER has a layout kind of its own — 17 — so a
//      conforming emitted closure carries NO kind-17 entry. A map and an
//      unbounded array have no fixed-form kind at all (their count is what the
//      data decides, and the fixed form's sizes are all compile constants), so
//      their refusal leaves nothing to look for; the pointer's absence is the
//      one assertion a reader can make. ("every table or type in it is declared
//      fixed" is the same fact one step earlier: a nested plain `table` is
//      itself a compile refusal — §2.2 — and a table's VARIABLE form would have
//      introduced a kind 17, so every kind-13 table the walk reaches below the
//      root is a fixed table by construction.)
//   2. "T is never in its own closure": the root entry IS T, and its id occurs
//      once, never again below it — a by-value self-reach is what a repeated
//      root id would have to be.
//   3. "…every table or type it reaches by value, recursively": the walk is
//      closed — a pre-order traversal from the root consumes exactly `count`
//      entries, so the recursion terminates and covers exactly the closure, with
//      no entry outside it and no unclosed subtree.
//
// one printed line per assertion; exit 0 green / 1 red.
//
// DERIVATION OF THE VECTOR: the layout is produced by
// `make build/tables-generated-js/.stamp` from tables/examples/Nested.schema,
// whose fixed table ArchiveConfig reaches another fixed table (RootConfig) by
// value, plus enums, a union, bounded arrays and fixed-point scalars — the
// corpora's richest single closure in one file. No byte here is hand-authored;
// the test reads the compiler's own output.

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const mod = await import(pathToFileURL(resolve(generated, "examples/NestedTable.js")).href);

const layout = mod.ArchiveConfigFixedLayout;
const layoutBytes = mod.ArchiveConfigFixedLayoutBytes;

let failures = 0;
function check(ok, line) {
  process.stdout.write(line + "\n");
  if (!ok) { failures++; }
}

// ---- parse the pre-order closure: u32 count, then 17-byte entries ----------
const u32 = (at) =>
  (layout[at] | (layout[at + 1] << 8) | (layout[at + 2] << 16) | (layout[at + 3] << 24)) >>> 0;
const count = u32(0);

check(layout.length === layoutBytes,
  `closure length: the layout array is as long as the module states (${layout.length})`);
check(4 + count * 17 === layoutBytes,
  `closure count: ${count} entries account for the stated ${layoutBytes} bytes`);

const kind = (i) => layout[4 + i * 17 + 8];
const idLo = (i) => u32(4 + i * 17);
const idHi = (i) => u32(4 + i * 17 + 4);
const children = (i) => u32(4 + i * 17 + 13);

// ---- clause 1: no pointer (kind 17) anywhere in the closure ---------------
let pointerAt = -1;
for (let i = 0; i < count; i++) { if (kind(i) === 17) { pointerAt = i; break; } }
check(pointerAt === -1,
  pointerAt === -1
    ? "no pointer in the closure: a pointer is a compile refusal, and no kind-17 entry appears"
    : `no pointer in the closure: a kind-17 entry appears at index ${pointerAt}`);

// ---- clause 3: the walk closes over exactly the closure -------------------
function subtree(i) {
  let pending = 1, n = 0, at = i;
  while (pending > 0 && at < count) {
    pending -= 1; n += 1; pending += children(at); at += 1;
    if (pending > count) { break; }
  }
  return n;
}

check(kind(0) === 13, `root is a table: the closure's first entry is T itself (kind ${kind(0)})`);
const walked = subtree(0);
check(walked === count,
  `closure closes: the pre-order walk from the root consumes all ${count} entries (walked ${walked})`);

// ---- clause 2: T is never in its own closure -------------------------------
const rootLo = idLo(0), rootHi = idHi(0);
let repeat = -1;
for (let i = 1; i < count; i++) {
  if (idLo(i) === rootLo && idHi(i) === rootHi) { repeat = i; break; }
}
check(repeat === -1,
  repeat === -1
    ? "T is never in its own closure: the root's id occurs once"
    : `T is never in its own closure: the root's id recurs at index ${repeat}`);

process.exit(failures === 0 ? 0 : 1);
