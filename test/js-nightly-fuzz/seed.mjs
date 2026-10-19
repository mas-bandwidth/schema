// seed.mjs — turn the Go fuzz corpus and the conformance wire vectors into
// input files for the JavaScript nightly fuzzer.
//
// The Go corpus lives in internal/fuzz/testdata/fuzz/<FuzzName>/<hash> as
//    go test fuzz v1
//    []byte("...")            (or string("..."))
// and the conformance vectors are the binary files under testdata/wire. Both
// are real inputs the repository already keeps; this script invents no corpus
// of its own, it only prefixes each input with the target's selector byte.
//
// Usage: node seed.mjs <go-fuzz-testdata-dir> <wire-vectors-dir> <out-dir> [max-bytes]
import fs from "node:fs";
import path from "node:path";

const [goDir, wireDir, outDir, maxArg] = process.argv.slice(2);
if (!goDir || !wireDir || !outDir) {
  console.error("usage: seed.mjs <go-fuzz-testdata-dir> <wire-vectors-dir> <out-dir> [max-bytes]");
  process.exit(2);
}
const maxBytes = Number(maxArg || 65536);
fs.mkdirSync(outDir, { recursive: true });

const decoder = (literal) => {
  // A Go double-quoted string literal is JSON-compatible for the escapes the
  // fuzz corpus uses; fall back to the raw text if it is not.
  try {
    return Buffer.from(JSON.parse(`"${literal}"`), "utf8");
  } catch {
    return Buffer.from(literal, "utf8");
  }
};

let seeds = 0;
const write = (name, body) => {
  if (body.length === 0 || body.length > maxBytes) return;
  const file = path.join(outDir, `${name.replace(/[^A-Za-z0-9_.-]/g, "_")}.seed`);
  fs.writeFileSync(file, Buffer.concat([Buffer.from([0]), body]));
  seeds++;
};

function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(p);
    else if (entry.isFile()) {
      const text = fs.readFileSync(p, "utf8");
      for (const m of text.matchAll(/(?:\[\]byte|string)\("((?:[^"\\]|\\.)*)"\)/g)) {
        write(`${entry.name}_${seeds}`, decoder(m[1]));
      }
    }
  }
}

if (fs.existsSync(goDir)) walk(goDir);

let vectors = 0;
function walkVectors(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) walkVectors(p);
    else if (entry.isFile() && entry.name.endsWith(".bin")) {
      write(`vector_${vectors++}_${entry.name}`, fs.readFileSync(p));
    }
  }
}
if (fs.existsSync(wireDir)) walkVectors(wireDir);

console.log(`seeds: ${seeds} inputs from ${goDir} and ${wireDir}`);
