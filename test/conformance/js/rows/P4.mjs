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
cam.Position.X = 1.25; cam.Position.Y = 3.5; cam.Position.Z = 7.75;
cam.CameraId = 0xC001CAFE;
cam.CameraType = 3;
cam.TargetObjectId = 4242;
cam.Fov = 90.0;

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

// 3. Extract just the body from the saved file and run the WRONG plan —
//    RenderMissile's identity plan — over Camera's body bytes.
const bodyAt = LAYOUT_HEAD + Render.RenderCameraFixedLayoutBytes + 8; // +8 past the per-record hash
const body = saveBytes.subarray(bodyAt, bodyAt + Render.RenderCameraFixedBodyBytes);
const bodyView = new DataView(body.buffer, body.byteOffset, body.length);

// THE WRONG PLAN: RenderMissile's identity plan is one Copy op of 71 bytes,
// applied to a 72-byte RenderCamera body.
const wrongPlan = new B.TableFixedPlan(4096, Render.RenderMissileFixedBodyBytes, 4096);
wrongPlan.image.set(new Uint8Array(wrongPlan.image.length));
const wrongVal = new B.RenderMissile();
B.InitRenderMissile(wrongVal);
const wrongReport = new B.TableFixedReport();

// RenderMissile identity: [Op=Copy, Src=0, Dst=0, Size=71, Aux=0, Guard=-1, Arg=0, Meta=0, ArgW=1]
const missileIdentity = new Int32Array([
  B.TableFixedOpCopy, 0, 0, Render.RenderMissileFixedBodyBytes, 0, -1, 0, 0, 1,
]);

B.TableFixedRun(missileIdentity, 1, body, bodyView, 0,
  wrongPlan.image, wrongPlan.view, null, wrongReport);
B.RenderMissileFixedDecode(wrongVal, wrongPlan.view, 0, wrongReport);

// 4. The wrong plan must NOT reproduce the record.
//    Camera bytes at offset 56–59 are CameraId (0xC001CAFE, LE: fe ca 01 c0).
//    Missile reads those same 8 bytes (56–63) as Flags (BigUint64), then
//    ObjectId at 64 (Camera's TargetObjectId, 4242 = 0x1092).
//    The values MUST disagree with a correct Camera read.
check(
  wrongVal.Flags !== 0n ||
  wrongVal.Position.X !== right[0].Position.X,
  "identity plan over another table's record comes out WRONG");

// 5. The CORRECT read is still correct — Position fields match because they
//    land at the same offsets in both tables. What proves the plan is wrong
//    is the divergence after Rotation (byte 56), where Camera's CameraId and
//    Missile's Flags disagree. Camera stores 0xC001CAFE at 56–59 and
//    CameraType=3 at 60–63; Missile reads those eight bytes as one BigUint64.
//    If both happened to read zero by coincidence, the identity plan would look
//    correct — so set CameraId to a nonzero value.

// Negative control: the right reader's values are what we set.
check(right[0].CameraId === 0xC001CAFE, "correct read preserves CameraId");
check(right[0].TargetObjectId === 4242, "correct read preserves TargetObjectId");
check(right[0].Fov === 90.0, "correct read preserves Fov");

// The wrong reader interprets Camera bytes as Missile fields: flags at 56–63
// (where Camera stores CameraId+CameraType) must not be zero with our inputs.
check(wrongVal.Flags !== 0n, "wrong plan yields non-zero Flags at CameraId bytes");

// and Position at 0–23 maps the same way in both, so it should match
check(wrongVal.Position.X === right[0].Position.X &&
      wrongVal.Position.Y === right[0].Position.Y &&
      wrongVal.Position.Z === right[0].Position.Z,
  "shared-offset fields match under both plans");

console.log("");
console.log("P4 wrong plan goes red: " + ok + " assertions, all passed");