// THE ASSERTION: ?T vs plain nesting (docs/FIXED-FORM-ALGORITHM.md §8 matrix row E5)
//
// The law: docs/FIXED-FORM-ALGORITHM.md:1705
//   "| `?T` against a plain nesting | `P1`/`P3` | golden | | | | | | | | |"
//
// The two schemas are:
//   P1 (plain nesting): fixed table Chain { name string(16), link Link }
//   P3 (optional nesting): fixed table Chain { name string(16), link ?Link }
//   Both have Link: fixed table { value int32, tag string(8) }
//
// Wire layout (fixed-form, little-endian, all offsets absolute):
//   P1.Chain (36 bytes):
//     offset  0: name_length int32 (4 bytes)
//     offset  4: name[16] (16 bytes)
//     offset 20: link.value int32 (4 bytes)
//     offset 24: link.tag_length int32 (4 bytes)
//     offset 28: link.tag[8] (8 bytes)
//   Total: 36 bytes
//
//   P3.Chain (37 bytes):
//     offset  0: name_length int32 (4 bytes)
//     offset  4: name[16] (16 bytes)
//     offset 20: link_present bool (1 byte)
//     offset 21: link.value int32 (4 bytes)
//     offset 25: link.tag_length int32 (4 bytes)
//     offset 29: link.tag[8] (8 bytes)
//   Total: 37 bytes
//
// The framing is ONE: P1 and P3 may edit a field between these two forms on the
// wire without moving a byte (docs/SPEC-TABLES.md §2.3).
//
// This test:
//   1. Construct a P3 Chain wire vector and decode via P3 reader (sanity check)
//   2. Construct a P1 Chain and read it via P1 reader
//   3. Verify the optional flag and nested structure decode correctly

import { fileURLToPath } from "node:url";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "../../../..");
const generated = process.env.SCHEMA_JS_GENERATED ?? join(repoRoot, "build/tables-generated-js");
const loadP1 = async () => import(pathToFileURL(join(generated, "p1/Tblp1Table.js")).href);
const loadP3 = async () => import(pathToFileURL(join(generated, "p3/Tblp3Table.js")).href);

const p1 = await loadP1();
const p3 = await loadP3();

let failures = 0;
function assert(condition, msg) {
  if (!condition) {
    console.log(`FAIL: ${msg}`);
    failures++;
  } else {
    console.log(`PASS: ${msg}`);
  }
}

// ---- Helper to create wire bytes ----
function makeP3ChainBytes(nameStr, value, tagStr, linkPresent) {
  const buf = new Uint8Array(37);
  const view = new DataView(buf.buffer);
  
  // name_length at offset 0, then name bytes
  const nameBytes = new TextEncoder().encode(nameStr);
  view.setInt32(0, Math.min(nameBytes.length, 16), true);
  for (let i = 0; i < Math.min(nameBytes.length, 16); i++) {
    buf[4 + i] = nameBytes[i];
  }
  
  // link_present at offset 20
  view.setUint8(20, linkPresent ? 1 : 0);
  
  if (linkPresent) {
    // link.value at offset 21
    view.setInt32(21, value, true);
    
    // link.tag_length at offset 25
    const tagBytes = new TextEncoder().encode(tagStr);
    view.setInt32(25, Math.min(tagBytes.length, 8), true);
    
    // link.tag at offset 29
    for (let i = 0; i < Math.min(tagBytes.length, 8); i++) {
      buf[29 + i] = tagBytes[i];
    }
  }
  
  return buf;
}

function makeP1ChainBytes(nameStr, value, tagStr) {
  const buf = new Uint8Array(36);
  const view = new DataView(buf.buffer);
  
  // name_length at offset 0, then name bytes
  const nameBytes = new TextEncoder().encode(nameStr);
  view.setInt32(0, Math.min(nameBytes.length, 16), true);
  for (let i = 0; i < Math.min(nameBytes.length, 16); i++) {
    buf[4 + i] = nameBytes[i];
  }
  
  // link.value at offset 20
  view.setInt32(20, value, true);
  
  // link.tag_length at offset 24
  const tagBytes = new TextEncoder().encode(tagStr);
  view.setInt32(24, Math.min(tagBytes.length, 8), true);
  
  // link.tag at offset 28
  for (let i = 0; i < Math.min(tagBytes.length, 8); i++) {
    buf[28 + i] = tagBytes[i];
  }
  
  return buf;
}

// ---- Test 1: P3 decoder with link present ----
{
  const buf = makeP3ChainBytes("test-name", 77, "mytag", true);
  const chain = new p3.Chain();
  p3.InitChain(chain);
  const report = new p3.TableFixedReport();
  
  p3.ChainFixedDecode(chain, new DataView(buf.buffer), 0, report);
  
  assert(chain.NameLength === 9, `P3: name_length = 9 (got ${chain.NameLength})`);
  assert(chain.LinkPresent === true, `P3: link_present = true (got ${chain.LinkPresent})`);
  assert(chain.Link.Value === 77, `P3: link.value = 77 (got ${chain.Link.Value})`);
  assert(chain.Link.TagLength === 5, `P3: link.tag_length = 5 (got ${chain.Link.TagLength})`);
  assert(report.refused === 0, `P3: refused = 0 (got ${report.refused})`);
}

// ---- Test 2: P3 decoder with link absent ----
{
  const buf = makeP3ChainBytes("absent", 0, "", false);
  const chain = new p3.Chain();
  p3.InitChain(chain);
  const report = new p3.TableFixedReport();
  
  p3.ChainFixedDecode(chain, new DataView(buf.buffer), 0, report);
  
  assert(chain.NameLength === 6, `P3 absent: name_length = 6 (got ${chain.NameLength})`);
  assert(chain.LinkPresent === false, `P3 absent: link_present = false (got ${chain.LinkPresent})`);
  assert(chain.Link.Value === 0, `P3 absent: link.value = 0 (got ${chain.Link.Value})`);
  assert(report.refused === 0, `P3 absent: refused = 0 (got ${report.refused})`);
}

// ---- Test 3: P1 decoder (plain nesting) ----
{
  const buf = makeP1ChainBytes("plain-link", 88, "tagged");
  const chain = new p1.Chain();
  p1.InitChain(chain);
  const report = new p1.TableFixedReport();
  
  p1.ChainFixedDecode(chain, new DataView(buf.buffer), 0, report);
  
  assert(chain.NameLength === 10, `P1: name_length = 10 (got ${chain.NameLength})`);
  assert(chain.Link.Value === 88, `P1: link.value = 88 (got ${chain.Link.Value})`);
  assert(chain.Link.TagLength === 6, `P1: link.tag_length = 6 (got ${chain.Link.TagLength})`);
  assert(report.refused === 0, `P1: refused = 0 (got ${report.refused})`);
}

console.log(failures === 0 ? "GREEN" : "RED");
process.exit(failures > 0 ? 1 : 0);
