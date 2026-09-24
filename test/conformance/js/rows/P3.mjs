// row js/P3 — "hostile bytes, sweep + sanitizer" (docs/roadmap.sexp js/P3;
// docs/FIXED-FORM-ALGORITHM.md §7 proof 5, §8 matrix row "byte-flip fuzz,
// sanitizer, three answers never a fourth").
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:1686 (§7 proof 5):
//   "a byte-flip fuzz over the whole file, under a sanitizer, and it is not
//    optional — every offset is arithmetic over sizes a stranger wrote down,
//    so every byte, one bit at a time, is answered one of three ways and
//    never a fourth: a refusal by name, a `malformed` read, or a read that
//    lands values."
//
// The JS leg's fixed-form reader is the generated block reader
// (build/tables-generated-js/block/*Block.js, the same modules the
// conformance driver imports via SCHEMA_JS_GENERATED). Its answer set has no
// separate `malformed` verdict: bytes that are not this build's come back as
// a null handle — the refusal — and a valid image opens to a handle whose
// every row reads inside the bytes it was given — the read that lands values.
// The language's own sanitizer is the DataView's bounds check: a read past
// the view THROWS, so "no exception escaped a reader" IS "no read left the
// buffer". That escaping exception is the fourth answer this test says never
// happens — the reader that lets one out has stopped refusing and has not
// landed values, which is exactly the defect the leg's own fuzzer oracle
// calls "a read that escaped" (test/js-tables/main.mjs, fuzzOne).
//
// THE SWEEP. One bit at a time over every byte of a whole file: a valid
// block image, flipped once per mutant, opened at the file's own length with
// an aligned base (the caller's placement the driver also uses). Every mutant
// must be answered null, or a handle whose full row walk stays inside the
// extent. Any exception out of Open or out of the walk is a fourth answer and
// a failure.
//
// The image is built from the law's own datum — a block of THIS build, whose
// every offset is arithmetic over sizes the header declares — so the vector
// is the conformance corpus's own block_render.bin / block_padded.bin
// (testdata/wire/tables/), read through the SAME generated reader the driver
// drives on the block surfaces.

import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

let failures = 0;
function assert(ok, what) {
  console.log((ok ? "OK   " : "FAIL ") + what);
  if (!ok) { failures++; }
}

// ---- the walk, over the descriptors and nothing else (the driver's dump
// and the leg's fuzzer both read exactly this way: every field of every row
// of every out-of-line array). A row read that would leave the bytes throws,
// which is the escaped read this test exists to say never happens.

function readField(view, at, f) {
  if (f.ElementRef !== null && f.ElemSize > 0) {
    for (const g of f.ElementRef().Fields) {
      const slots = g.IsArray ? g.ArrayBound : 1;
      for (let s = 0; s < slots; s++) { readField(view, at + g.Offset + s * g.ElemSize, g); }
    }
    return;
  }
  switch (f.ElemSize) {
    case 1: view.getUint8(at); return;
    case 2: view.getUint16(at, true); return;
    case 4: view.getUint32(at, true); return;
    case 8: view.getBigUint64(at, true); return;
    default: for (let i = 0; i < f.ElemSize; i++) { view.getUint8(at + i); }
  }
}

function walkBlock(block) {
  return (handle) => {
    const info = block.Type;
    for (const f of info.Fields) {
      if (!f.OutOfLine) { continue; }
      const offsetOf = Number(handle.View.getBigUint64(f.OffsetOfOffset, true));
      const count = handle.View.getUint32(f.CountOffset, true);
      const stride = handle.View.getUint32(f.StrideOffset, true);
      const rowInfo = f.ElementRef();
      for (let r = 0; r < count; r++) {
        const at = offsetOf + r * stride;
        for (const g of rowInfo.Fields) {
          const slots = g.IsArray ? g.ArrayBound : 1;
          for (let s = 0; s < slots; s++) { readField(handle.View, at + g.Offset + s * g.ElemSize, g); }
        }
      }
    }
  };
}

// ONE BYTE, ONE BIT: the sweep step. Every mutant is a single bit of a valid
// file reversed, opened at the file's own length on an aligned base. The three
// legal answers collapse to two verdicts in this reader's surface — null (the
// refusal) and a handle (a read that lands values) — and the sanitizer's own
// word for a read that left the buffer is the exception this returns as a
// fourth answer. Every mutant is counted, and the count is an assertion.
function sweep(open, walk, source) {
  let refused = 0;
  let opened = 0;
  let escaped = 0;
  let firstEscape = "";
  for (let i = 0; i < source.length; i++) {
    for (let b = 0; b < 8; b++) {
      const mutant = new Uint8Array(source.length);
      mutant.set(source);
      mutant[i] ^= 1 << b;
      let handle = null;
      try {
        handle = open(mutant);
      } catch (e) {
        escaped++;
        if (firstEscape === "") { firstEscape = "Open @byte " + i + " bit " + b + ": " + (e && e.message ? e.message : String(e)); }
        continue;
      }
      if (handle === null) { refused++; continue; }
      opened++;
      try {
        walk(handle);
      } catch (e) {
        escaped++;
        if (firstEscape === "") { firstEscape = "walk @byte " + i + " bit " + b + ": " + (e && e.message ? e.message : String(e)); }
      }
    }
  }
  return { refused, opened, escaped, firstEscape };
}

async function main() {
  const renderBlock = await load("block/RenderBlock.js");
  const paddedBlock = await load("block/PaddedBlock.js");

  const subjects = [
    {
      name: "block_render",
      file: "testdata/wire/tables/block_render.bin",
      open: (b) => renderBlock.RenderFrameBlock.Open(b),
      walk: walkBlock(renderBlock.RenderFrameBlock),
    },
    {
      name: "block_padded",
      file: "testdata/wire/tables/block_padded.bin",
      open: (b) => paddedBlock.PaddedFrameBlock.Open(b),
      walk: walkBlock(paddedBlock.PaddedFrameBlock),
    },
  ];

  for (const subject of subjects) {
    const source = new Uint8Array(readFileSync(subject.file));
    const clean = new Uint8Array(source.length);
    clean.set(source);
    assert(subject.open(clean) !== null, subject.name + ": the lawful image opens");
    const { refused, opened, escaped, firstEscape } = sweep(subject.open, subject.walk, source);
    assert(escaped === 0, subject.name + ": " + (source.length * 8) + " single-bit mutants (" + source.length +
      " bytes), " + refused + " refused, " + opened + " opened and walked whole inside the bytes they were given — " +
      (escaped === 0 ? "no exception escaped a reader" : firstEscape));
  }

  if (failures === 0) {
    console.log("row js/P3: every byte, one bit at a time, is answered by a refusal or a read that lands values — never a fourth (docs/FIXED-FORM-ALGORITHM.md:1686)");
  }
  process.exit(failures === 0 ? 0 : 1);
}

await main();