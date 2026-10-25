package jstable

// THE WIRE-FUZZ CAUSE JS1 (js-fix-simstate-kind-mismatch-not-widened): a kind
// pass on scalars.SimState rewrites a field's wire kind. The JavaScript leg read
// it as a WIDEN — reporting 0,0,1,0,0,true — where the Go oracle counts a KIND
// MISMATCH, 0,1,0,0,0,true. The JS widening ladder's 128-bit rungs accepted
// cross-sign and float payloads (kind < declared), where ir.TableKindWidens
// accepts only its own signedness ladder (docs/SPEC-TABLES.md §4). This test
// loads the reproduced mutant (corpus seed scalars_full, pass kind #391, run
// seed 24845619678, sha256 begins 5e2f3a26) with the JS leg and pins the report
// counters and the re-saved bytes to the Go oracle's.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestSimStateKindMismatchNotWidenedJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)

	const mutantHex = "01011481021601004cff031700802f7500000000041800c0c0bdf0ffffffffffffffffffffff051640420f000619ef071b0000e703081cfffffeffffffffff091d00807f841e00000000000000000000000a1bffffffff0b12000000000000000000000000f0ffffff0c06fff1052a0100000000000000000000000d13ffffffffffffffffffffffffffffffff0e160080ffff0f0e0e1603008001000000f8ffffff0700100e061a028000006411101617021208000000009cffffff1308ffffffff63000000140e2213020100000000000000000000000000000000000000000000000100000000000000150d1b16170080ffffffffffff171700400c0000000000181bffff670100190d07181b000001000000c6af9b2eef88501e38861506cdb81a404ad7a1fc263abf4c58b8de05f3a11d89b5253e6a6c26d97fe4ad77811124c9fb400e20a08a498122e1d03c0919c07d8bb10a7bcea257371f6fe6b4286790e5234226af0879dd1bd6152fef40914ffdf21267e37866fdfc23911d1ab7fbb9ac6adcdd483b6acab1e31e418af06e4b49b1b8679c0d849e9632674d02864c1564afcd5002864c1764af6de14a1b30acf15a647095a80dd82f8c071702864cf563af541502864cf463af750fca90918228b1981f0eb28af728431900000000000000"
	mutant, err := hex.DecodeString(mutantHex)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	u := loadUnitPaths(t, []string{filepath.Join(repo, "tables", "scalars")})

	// THE GO ORACLE: decode the mutant and re-encode the decoded instance. The
	// mutant is a kind_mismatch on the rewritten field, which the reference reads
	// and reports (malformed true when the walk stops past it), so `ok` is not
	// required to be true.
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("SimState"))
	var rep tabletext.Report
	ok, err := tablewire.DecodeRegion(m, inst, mutant, &rep)
	if err != nil {
		t.Fatalf("the oracle refused the kind #391 mutant: %v", err)
	}
	_ = ok
	if rep.KindMismatch != 1 || rep.Widened != 0 {
		t.Fatalf("oracle counters: kind_mismatch=%d widened=%d, want 1 0", rep.KindMismatch, rep.Widened)
	}
	encoded, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle encode: %v", err)
	}

	// THE JAVASCRIPT LEG: generate the scalars unit and run the same read.
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := fmt.Sprintf(`
import { SimStateLoad, SimStateSave, TableVariableReport } from "./ScalardemoVariable.js";
const hex = "%s";
const bytes = new Uint8Array(hex.length / 2);
for (let i = 0; i < bytes.length; i++) { bytes[i] = parseInt(hex.substr(i * 2, 2), 16); }
const report = new TableVariableReport();
const v = SimStateLoad(bytes, report);
if (!v) { console.log("load returned null"); process.exit(1); }
const saved = SimStateSave(v);
let savedHex = "";
for (let i = 0; i < saved.length; i++) { savedHex += saved[i].toString(16).padStart(2, "0"); }
console.log(JSON.stringify({ kindMismatch: report.kindMismatch, widened: report.widened, malformed: report.malformed, savedHex }));
`, mutantHex)
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	probeOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node probe: %v\n%s", err, probeOut)
	}
	var res struct {
		KindMismatch int    `json:"kindMismatch"`
		Widened      int    `json:"widened"`
		SavedHex     string `json:"savedHex"`
	}
	if err := json.Unmarshal(probeOut, &res); err != nil {
		t.Fatalf("could not parse probe output %q: %v", string(probeOut), err)
	}
	if res.KindMismatch != 1 || res.Widened != 0 {
		t.Fatalf("JS counters: kind_mismatch=%d widened=%d, want 1 0 (oracle)", res.KindMismatch, res.Widened)
	}
	saved, err := hex.DecodeString(res.SavedHex)
	if err != nil {
		t.Fatalf("bad saved hex: %v", err)
	}
	if !bytesEqual(saved, encoded) {
		t.Fatalf("JS re-save (%d bytes) != oracle re-encode (%d bytes)", len(saved), len(encoded))
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
