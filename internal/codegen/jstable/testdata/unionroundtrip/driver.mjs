// THE UNION ROUND TRIP (internal/codegen/jstable/unionimports_test.go): every
// union the table corpus declares is CONSTRUCTED, each arm's storage is checked
// against the class the DEFINING unit exports, and each arm in turn is written
// through the holder table's form and read back. node's own test runner and
// assert, nothing else (docs/SPEC-TABLES.md §2.6).
//
// usage: node driver.mjs spec.json
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));

function load(dir, module) {
  return import(pathToFileURL(join(spec.root, dir, module)).href);
}

// fill sets every leaf of a value to a non-zero value its declaration accepts
// for any bound the corpus declares: 1 for a number and a count, 1n for a
// BigInt, true, "A", and an "A" in the first byte of a text or byte buffer. A
// nested union is left at None (its tag is what selects which arm a read
// fills, so a dormant arm filled here would never come back), and a counted
// array is filled only below its count (the elements past it never ride).
function fill(v, limit) {
  if (v === null || typeof v !== "object") { return; }
  if (Array.isArray(v)) {
    const n = limit === undefined ? v.length : Math.min(limit, v.length);
    for (let i = 0; i < n; i++) {
      if (typeof v[i] === "number") { v[i] = 1; } else if (typeof v[i] === "bigint") { v[i] = 1n; } else { fill(v[i]); }
    }
    return;
  }
  if (ArrayBuffer.isView(v)) {
    if (v.length > 0) { v[0] = typeof v[0] === "bigint" ? 1n : 0x41; }
    return;
  }
  if ("Type" in v) { return; }
  const keys = Object.keys(v);
  for (const k of keys) { if (typeof v[k] !== "object") { fillKey(v, k); } }
  for (const k of keys) { if (typeof v[k] === "object") { fillKey(v, k); } }
}

// fillKey fills one member. A member's count (<k>Count) is a scalar, so the
// scalar pass has already set it when its array is reached.
function fillKey(v, k) {
  switch (typeof v[k]) {
    case "number": v[k] = 1; break;
    case "bigint": v[k] = 1n; break;
    case "boolean": v[k] = true; break;
    case "string": v[k] = "A"; break;
    default: fill(v[k], typeof v[k + "Count"] === "number" ? v[k + "Count"] : undefined);
  }
}

// fillArm fills one arm's storage on the union value: a class arm is its own
// member, a direct arm is its member and the length or count beside it.
function fillArm(u, arm) {
  if (arm.kind === "void") { return; }
  for (const k of [arm.member + "Length", arm.member + "Count", arm.member]) {
    if (k in u) { fillKey(u, k); }
  }
}

for (const c of spec.cases) {
  test(`${c.dir} ${c.union}`, async (t) => {
    const mod = await load(c.dir, c.module);
    const Union = mod[c.union];
    const Tag = mod[c.union + "Type"];
    assert.equal(typeof Union, "function", `${c.module} exports no class ${c.union}`);

    await t.test("constructs", async () => {
      const v = new Union();
      assert.equal(v.Type, Tag.None);
      for (const arm of c.arms) {
        if (arm.kind !== "class") { continue; }
        const defining = await load(c.dir, arm.module);
        assert.equal(typeof defining[arm.class], "function", `${arm.module} exports no class ${arm.class}`);
        assert.ok(v[arm.member] instanceof defining[arm.class],
          `${c.union}.${arm.member} is not an instance of ${arm.class} from ${arm.module}, the module that defines it`);
      }
    });

    if (!c.holder) { return; }
    const h = c.holder;
    const verbs = await load(c.dir, h.module);
    const home = await load(c.dir, h.home);
    for (const arm of c.arms) {
      await t.test(`round trip ${arm.name} through ${h.root} (${h.form} form)`, () => {
        if (h.form === "fixed") {
          const holder = new home[h.root]();
          const u = holder[h.field];
          assert.ok(u instanceof Union, `${h.root}.${h.field} is not a ${c.union}`);
          u.Type = Tag[arm.member];
          fillArm(u, arm);
          const bytes = new Uint8Array(verbs[h.root + "FixedMeasure"](1));
          assert.equal(verbs[h.root + "FixedSave"]([holder], 1, bytes), bytes.length, "the fixed save refused");
          const back = [new home[h.root]()];
          const report = new home.TableFixedReport();
          const n = verbs[h.root + "FixedLoad"](back, 1, bytes, bytes.length, verbs[h.root + "FixedNewPlan"](), report);
          assert.equal(n, 1, `the fixed load refused: ${JSON.stringify(report, (_, x) => typeof x === "bigint" ? String(x) : x)}`);
          assert.deepStrictEqual(back[0], holder);
          return;
        }
        // THE VARIABLE FORM holds a union as { arm, value }; the value is the
        // union class's own arm storage, so the class it constructs must be
        // the variable home's or the codec does not know it.
        const u = new Union();
        u.Type = Tag[arm.member];
        fillArm(u, arm);
        const holder = new home[h.root]();
        holder[h.field] = { arm: arm.name, value: arm.kind === "void" ? null : u[arm.member] };
        const bytes = verbs[h.root + "Save"](holder);
        assert.notEqual(bytes, null, "the variable save refused");
        const report = new home.TableVariableReport();
        const back = verbs[h.root + "Load"](bytes, report);
        assert.notEqual(back, null, `the variable load refused: ${JSON.stringify(report, (_, x) => typeof x === "bigint" ? String(x) : x)}`);
        assert.equal(back[h.field].arm, arm.name);
        if (arm.kind !== "void") {
          assert.deepStrictEqual(back[h.field].value, holder[h.field].value);
        }
      });
    }
  });
}
