import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";

const diagnostic = /^.+\(\d+,\d+\): error TS(\d+): .+$/;

function parseBaseline(raw) {
  const result = Object.create(null);
  if (!raw) throw new Error("empty diagnostic baseline");
  for (const pair of raw.split(",")) {
    const match = pair.match(/^(\d+):(\d+)$/);
    if (!match || Object.hasOwn(result, match[1])) {
      throw new Error("invalid or duplicate baseline entry: " + pair);
    }
    result[match[1]] = Number(match[2]);
  }
  return result;
}

function inspect(log, exitCode, baseline, expectedFiles) {
  const lines = log.split(/\r?\n/).filter(Boolean);
  const counts = Object.create(null);
  const errors = [];
  let parsed = 0;
  const seenFiles = new Set();
  let inDiagnostic = false;

  for (const line of lines) {
    const match = line.match(diagnostic);
    if (match) {
      parsed++;
      counts[match[1]] = (counts[match[1]] || 0) + 1;
      inDiagnostic = true;
      continue;
    }
    if (inDiagnostic && /^ {2,}\S/.test(line)) continue;
    inDiagnostic = false;
    const candidate = path.resolve(line);
    if (/\.(?:[cm]?js|[cm]?ts)$/.test(candidate) && fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
      seenFiles.add(candidate);
      continue;
    }
    errors.push("unparsed diagnostic or tool output: " + line);
  }

  for (const file of expectedFiles) {
    if (!seenFiles.has(path.resolve(file))) errors.push("NOT ANALYSED " + file);
  }
  if (expectedFiles.length === 0) errors.push("empty expected input set");

  if (exitCode === 0) {
    if (parsed !== 0) errors.push("tsc returned success with diagnostic output");
  } else if (exitCode !== 1 && exitCode !== 2) {
    errors.push("unexpected tsc exit status " + exitCode + " (only 1 or 2 denote TypeScript diagnostics)");
  } else if (parsed === 0) {
    errors.push("tsc returned a diagnostics status without parsed diagnostics");
  }

  for (const code of Object.keys(counts)) {
    const max = baseline[code] || 0;
    if (counts[code] > max) errors.push("TS" + code + " count " + counts[code] + " exceeds baseline " + max);
  }
  return { counts, errors, parsed, total: lines.length };
}

function selfTest() {
  const baseline = parseBaseline("7006:1");
  const parent = path.join(process.cwd(), "build", "schema-js-ratchet-self-test");
  fs.mkdirSync(parent, { recursive: true });
  const dir = fs.mkdtempSync(path.join(parent, "case-"));
  const file = path.join(dir, "example.js");
  fs.writeFileSync(file, "const example = 1;\n");
  const one = file + "(1,1): error TS7006: implicit any";
  const multiline = file + "(2,1): error TS2322: type mismatch\n  Type 'boolean' is not assignable to type 'number'.";
  const fileList = file;
  const accepted = inspect(fileList + "\n" + one + "\n" + multiline, 2, baseline, [file]);
  assert.deepEqual(accepted.errors, ["TS2322 count 1 exceeds baseline 0"], "status 2 and indented compiler continuation are parsed as diagnostics");
  const baselineRun = inspect(fileList + "\n" + one + "\n" + file + "(2,1): error TS7006: second implicit any\n  Type 'boolean' is not assignable to type 'number'.", 2, baseline, [file]);
  assert.deepEqual(baselineRun.errors, ["TS7006 count 2 exceeds baseline 1"], "multiline same-code growth remains enforced");

  const partialToolFailure = inspect(fileList + "\n" + one + "\nTypeScript worker failed after partial output", 2, baseline, [file]);
  assert(partialToolFailure.errors.some(x => x.includes("unparsed diagnostic or tool output")),
    "partial baseline diagnostics plus a tool failure must fail closed");

  const sameCodeGrowth = inspect(fileList + "\n" + one + "\n" + file + "(2,1): error TS7006: implicit any", 1, baseline, [file]);
  assert(sameCodeGrowth.errors.some(x => x.includes("TS7006 count 2 exceeds baseline 1")),
    "growth in a baseline diagnostic code must fail");

  const interrupted = inspect(fileList + "\n" + one, 143, baseline, [file]);
  assert(interrupted.errors.some(x => x.includes("unexpected tsc exit status")),
    "a signal-like status must not be treated as a diagnostics completion");

  const orphanContinuation = inspect(fileList + "\n  TypeScript worker failed", 2, baseline, [file]);
  assert(orphanContinuation.errors.some(x => x.includes("unparsed diagnostic or tool output")), "orphaned indented text must fail");

  const unlocated = inspect(fileList + "\nerror TS7006: malformed diagnostic", 1, baseline, [file]);
  assert(unlocated.errors.some(x => x.includes("unparsed diagnostic or tool output")),
    "unlocated diagnostic-shaped output must fail");

  const clean = inspect(fileList, 0, baseline, [file]);
  assert.deepEqual(clean.errors, [], "a clean completed run is accepted");
  fs.rmSync(dir, { recursive: true, force: true });
  console.log("schema-js checkJs ratchet self-test: TypeScript 5.9.3 status 2, multiline diagnostics, partial tool failure, same-code growth, interruption, malformed output, and clean/baseline runs PASS");
}

function main(args) {
  if (args.length === 1 && args[0] === "--self-test") {
    selfTest();
    return;
  }

  const options = Object.create(null);
  for (let i = 0; i < args.length; i += 2) {
    if (!args[i]?.startsWith("--") || i + 1 >= args.length) throw new Error("invalid arguments");
    options[args[i].slice(2)] = args[i + 1];
  }
  if (!options.log || !options.files || options.baseline === undefined || !/^-?\d+$/.test(options.exit || "")) {
    throw new Error("usage: node schema_js_checkjs_ratchet.mjs --log FILE --files FILE --baseline CODE:COUNT,... --exit STATUS");
  }

  const result = inspect(fs.readFileSync(options.log, "utf8"), Number(options.exit), parseBaseline(options.baseline), fs.readFileSync(options.files, "utf8").split(/\r?\n/).filter(Boolean));
  console.log("checkJs diagnostic counts: " + JSON.stringify(result.counts) + "; total " + result.parsed + "; tsc exit " + options.exit);
  for (const error of result.errors) console.log("FAIL " + error);
  if (result.errors.length) process.exitCode = 1;
}

try {
  main(process.argv.slice(2));
} catch (error) {
  console.error("schema-js checkJs ratchet: FAIL " + error.message);
  process.exitCode = 1;
}
