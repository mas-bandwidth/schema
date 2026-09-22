// E6.mjs — Renaming identity test (new)
// This is a minimal conformance test for the E6 item: "Renaming uses the declared identity".
// It exercises the generated JS tables surface and asserts that the table form for the
// v2 backend matches the expected identity (form 3).

import { pathToFileURL } from "node:url";
import { resolve } from "node:path";

const generated = "build/tables-generated-js";
const imp = (p) => import(pathToFileURL(resolve(generated, p)).href);

(async () => {
  try {
    const mod = await imp("v2/Tblv2Table.js");
    // Basic sanity: ensure the known form constant is present and equals 3
    const form = mod.TableFixedForm ?? null;
    const ok = typeof form === "number" && form === 3;
    console.log(ok ? "PASS: E6 basic form constant matches 3" : `FAIL: E6 form constant mismatch (form=${form})`);
    process.exit(ok ? 0 : 1);
  } catch (e) {
    console.log("FAIL: E6 import/test error", e?.message ?? e);
    process.exit(1);
  }
})();
