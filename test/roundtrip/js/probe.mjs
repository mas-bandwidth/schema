// The round-trip gate's JavaScript probe (test/roundtrip):
//   node probe.mjs <generated dir> <inputs dir>
// Every call is the generated API, found by name in the generated modules:
// the plain pair, JSON and retention in <Package>Variable.js, the fixed form in
// the per-file <File>Table.js that exports <Root>FixedLoad.
import { readFileSync, writeFileSync, readdirSync, existsSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { join } from "node:path";

const [gen, inputs] = process.argv.slice(2);
const files = readdirSync(gen);
const variableFile = files.find((f) => f.endsWith("Variable.js"));
if (!variableFile) {
  throw new Error("the generated unit has no <Package>Variable.js");
}
const V = await import(pathToFileURL(join(gen, variableFile)));
const P = await import(pathToFileURL(join(gen, variableFile.replace(/Variable\.js$/, "Table.js"))));
const fixedHome = {};
for (const f of files.filter((f) => f.endsWith("Table.js"))) {
  const m = await import(pathToFileURL(join(gen, f)));
  for (const k of Object.keys(m)) {
    if (k.endsWith("FixedLoad")) {
      fixedHome[k.slice(0, -"FixedLoad".length)] = m;
    }
  }
}

const say = (root, name, verb, verdict) => console.log(`RT ${root} ${name} ${verb} ${verdict}`);
const read = (...p) => {
  const path = join(inputs, ...p);
  return existsSync(path) ? new Uint8Array(readFileSync(path)) : null;
};
const keep = (root, name, ext, bytes) => writeFileSync(join(inputs, "out", root, name + ext), bytes);
const same = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
const why = (r) => JSON.stringify(r, (k, x) => (typeof x === "bigint" ? String(x) : x));
// call answers f's verdict, and a throw as a refusal by name.
const call = (f) => {
  try {
    return f();
  } catch (e) {
    return "refused:threw " + String(e && e.message ? e.message : e).replace(/\s+/g, " ");
  }
};

for (const line of readFileSync(join(inputs, "cases.txt"), "utf8").split("\n")) {
  if (!line) continue;
  const [root, name, cls] = line.split(" ");
  const fn = (suffix) => V[root + suffix];
  if (!fn("FromJson")) {
    say(root, name, "probe", "refused:the unit generated no such root");
    continue;
  }
  const text = new TextDecoder().decode(read("samples", root, name + ".json"));
  let v = null;
  const fr = new V.TableVariableReport();
  const fromjson = call(() => {
    v = fn("FromJson")(text, fr);
    return v == null ? "refused:" + why(fr) : "ok";
  });
  say(root, name, "fromjson", fromjson);
  if (v == null) continue;
  const want = fn("ToJson")(v);
  const loadVerdict = (bytes) => {
    const r = new V.TableVariableReport();
    const got = fn("Load")(bytes, r);
    return got == null ? "refused:" + why(r) : fn("ToJson")(got) === want ? "ok" : "diff";
  };
  const eng = read("engine", root, name + ".bin");
  let wire = null;
  say(root, name, "save", call(() => {
    wire = fn("Save")(v);
    if (wire == null) return "refused:" + String(fn("MeasureReason") ? fn("MeasureReason")(v) : "");
    keep(root, name, ".save", wire);
    return "ok";
  }));
  if (wire != null) {
    say(root, name, "load", call(() => loadVerdict(wire)));
  }
  if (eng != null) {
    say(root, name, "loadeng", call(() => loadVerdict(eng)));
  }
  const H = fixedHome[root];
  if (cls === "fixed" && H) {
    say(root, name, "fixedsave", call(() => {
      const out = new Uint8Array(H[root + "FixedMeasure"](1));
      const n = H[root + "FixedSave"]([v], 1, out);
      if (n !== out.length) return "refused:" + n;
      keep(root, name, ".fixed", out);
      return "ok";
    }));
    if (eng != null) {
      say(root, name, "fixedloadeng", call(() => {
        const x = new P[root]();
        P["Init" + root](x);
        const values = [x];
        const report = new P.TableFixedReport();
        const n = H[root + "FixedLoad"](values, 1, eng, eng.length, H[root + "FixedNewPlan"](), report);
        if (n !== 1) return "refused:" + why(report);
        return fn("ToJson")(values[0]) === want ? "ok" : "diff";
      }));
    }
  }
  if (cls === "variable") {
    if (wire != null) {
      say(root, name, "retain", call(() => {
        const store = new V.TableVariableRetain(new Uint8Array(1 << 16), new Array(1 << 13));
        const r = new V.TableVariableReport();
        const got = fn("LoadRetain")(wire, store, r);
        if (got == null) return "refused:" + why(r);
        const sr = new V.TableVariableReport();
        const back = fn("SaveRetain")(got, store, sr);
        if (back == null) return "refused:" + why(sr);
        return fn("ToJson")(got) === want && same(back, wire) ? "ok" : "diff";
      }));
    }
    say(root, name, "json", call(() => {
      const r = new V.TableVariableReport();
      const j = fn("FromJson")(want, r);
      return j == null ? "refused:" + why(r) : fn("ToJson")(j) === want ? "ok" : "diff";
    }));
  }
}
