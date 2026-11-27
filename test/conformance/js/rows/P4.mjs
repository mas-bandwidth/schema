// P4: wrong plan goes red — this build's identity plan over another schema's
// record must come out WRONG.
//
// The fixed form's safety rests on the plan: a reader handed the wrong plan
// for a record body must disagree with any correct read of those same bytes.
// A test that never watched a wrong plan fail is a test that never checked
// the right one worked (docs/FIXED-FORM-ALGORITHM.md:1685).
//
// This test uses two tables with the same body size but different layouts,
// RenderCamera (72 bytes) and RenderMissile (71 bytes, one short of Camera),
// to demonstrate that the identity plan for one applied to the other's record
// body produces values that do not match what the correct reader sees.

import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const generated = process.env.SCHEMA_JS_GENERATED ?? "build/tables-generated-js";
const load = (path) => import(pathToFileURL(resolve(generated, path)).href);

const Render = await load("block/RenderTable.js");
const B = await load("block/BlockdemoTable.js");

let ok = 0;

function check(cond, label) {
  if (cond) {
    ok++;
    console.log("ok " + ok + " P4 " + label);
    return;
  }
  console.log("FAIL P4 " + label);
  process.exit(1);
}

const LAYOUT_HEAD = B.TableFixedHeaderBytes + B.TableFixedLayoutHeaderBytes;

// 1. Save one RenderCamera record with distinctive values.
const cam = new B.RenderCamera();
B.InitRenderCamera(cam);
cam.position.x = 1.25; cam.position.y = 3.5; cam.position.z = 7.75;
cam.cameraId = 0xC001CAFE;
cam.cameraType = 3;
cam.targetObjectId = 4242;
cam.fov = 90.0;

const saveBytes = new Uint8Array(Render.RenderCameraFixedMeasure(1));
check(Render.RenderCameraFixedSave([cam], 1, saveBytes) === Render.RenderCameraFixedMeasure(1),
  "save one RenderCamera record");

// 2. Read it back correctly through the full FixedLoad path.
const right = [new B.RenderCamera()];
const rightReport = new B.TableFixedReport();
const rightPlan = new B.TableFixedPlan(4096, Render.RenderCameraFixedBodyBytes, 4096);
const nRight = Render.RenderCameraFixedLoad(right, 1, saveBytes, saveBytes.length,
  rightPlan, rightReport);
check(nRight === 1 && rightReport.refused === B.TableFixedRefusal.None,
  "correct FixedLoad reads one record with no refusal");

// 3. THE WRONG PLAN: RenderMissile's identity plan is one Copy op of 71
//    bytes, run over Camera's 72-byte record body, then RenderMissile's own
//    decode over the image it produced — the same two steps a load takes.
//
//    The wrong plan now goes red in ONE OF TWO WAYS, and both are asserted.
//    When Camera's bytes happen to fall outside a Missile field's declared set
//    the bounds pass refuses `out_of_range` before the decode lands a field
//    (docs/FIXED-FORM-ALGORITHM.md §4.6, line 1218, and §4.5: "writes not one
//    destination byte"); this row predates that pass and once expected the
//    decode to land garbage there. Where the bytes are inside every declared
//    set, the decode lands, and the values come out WRONG.
const bodyAt = LAYOUT_HEAD + Render.RenderCameraFixedLayoutBytes + 8; // +8 past the per-record hash
const missileIdentity = new Int32Array([
  B.TableFixedOpCopy, 0, 0, Render.RenderMissileFixedBodyBytes, 0, -1, 0, 0, 1,
]);

function wrongRead(file) {
  const body = file.subarray(bodyAt, bodyAt + Render.RenderCameraFixedBodyBytes);
  const bodyView = new DataView(body.buffer, body.byteOffset, body.length);
  const wrongPlan = new B.TableFixedPlan(4096, Render.RenderMissileFixedBodyBytes, 4096);
  wrongPlan.image.fill(0);
  const val = new B.RenderMissile();
  B.InitRenderMissile(val);
  const report = new B.TableFixedReport();
  B.TableFixedRun(missileIdentity, 1, body, bodyView, 0, wrongPlan.image, wrongPlan.view, null, report);
  check(report.refused === 0, "the wrong plan's copy itself refuses nothing (it is a plain copy)");
  B.RenderMissileFixedDecode(val, wrongPlan.view, 0, report);
  return { val, report };
}

const fresh = new B.RenderMissile();
B.InitRenderMissile(fresh);
const untouched = (v) => JSON.stringify(v, (k, x) => typeof x === "bigint" ? x.toString() : x) ===
  JSON.stringify(fresh, (k, x) => typeof x === "bigint" ? x.toString() : x);

// 3a. Camera's Fov 90.0 is 0x42B40000, so Camera body byte 70 is 0xB4 = 180,
//     which Missile reads as `team` — declared with 5 variants (0..4). The
//     bounds pass refuses it BY NAME and the decode lands nothing.
{
  check(saveBytes[bodyAt + 70] === 0xb4, "the vector: Camera body byte 70 is 0xB4, Missile's team byte");
  const { val, report } = wrongRead(saveBytes);
  check(report.refused === B.TableFixedRefusal.OutOfRange,
    "wrong plan over Camera (Fov 90): team 180 past the 5 variants refuses out_of_range (refused=" +
    B.TableFixedRefusalName(report.refused) + ")");
  check(untouched(val),
    "wrong plan over Camera (Fov 90): the refusing decode lands not one field (§4.5)");
  check(report.oor === 0, "wrong plan over Camera (Fov 90): the refusal counts nothing");
}

// 3b. A Camera whose Fov is 0.0 leaves Missile's two tag bytes (69, 70) zero,
//     inside every declared set, so the decode lands — and lands WRONG.
//     Camera stores CameraId 0xC001CAFE at 56..59 and CameraType 3 at 60..63;
//     Missile reads those eight bytes as one BigUint64 Flags, so a correct
//     plan's Flags (zero: Camera has none) and the wrong plan's disagree.
{
  const cam0 = new B.RenderCamera();
  B.InitRenderCamera(cam0);
  cam0.position.x = 1.25; cam0.position.y = 3.5; cam0.position.z = 7.75;
  cam0.cameraId = 0xC001CAFE;
  cam0.cameraType = 3;
  cam0.targetObjectId = 4242;
  cam0.fov = 0.0;
  const file0 = new Uint8Array(Render.RenderCameraFixedMeasure(1));
  check(Render.RenderCameraFixedSave([cam0], 1, file0) === file0.length, "save the Fov-0 RenderCamera record");
  const { val, report } = wrongRead(file0);
  check(report.refused === 0, "wrong plan over Camera (Fov 0): every byte is inside Missile's sets, so it lands");
  check(val.flags === ((3n << 32n) | 0xC001CAFEn),
    "wrong plan yields non-zero Flags at CameraId bytes (0x" + val.flags.toString(16) + ")");
  check(val.objectId === 4242,
    "wrong plan reads Camera's TargetObjectId as Missile's ObjectId — another field's value");
  // Position at 0–23 maps the same way in both, so it matches: the plan is
  // wrong where the layouts diverge and only there.
  check(val.position.x === 1.25 && val.position.y === 3.5 && val.position.z === 7.75,
    "shared-offset fields match under both plans");
}

// 4. NEGATIVE CONTROL: the right reader's values are what was written.
check(right[0].cameraId === 0xC001CAFE, "correct read preserves CameraId");
check(right[0].targetObjectId === 4242, "correct read preserves TargetObjectId");
check(right[0].fov === 90.0, "correct read preserves Fov");

console.log("");
console.log("P4 wrong plan goes red: " + ok + " assertions, all passed");
