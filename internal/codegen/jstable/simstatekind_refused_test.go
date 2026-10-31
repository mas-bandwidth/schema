package jstable

// THE WIRE-FUZZ CAUSE JS1 on scalars.SimState: a kind pass mutant (corpus seed
// scalars_full, pass kind #391, run seed 24845619678, sha256 begins 5e2f3a26)
// that the Go oracle reads as damaged. DAMAGE DISCARDS THE WHOLE LOAD
// (docs/SPEC-TABLES.md §4): the JavaScript leg owes the refusal, malformed,
// and no counter or re-saved byte is compared.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestSimStateKindPassDamageRefusedJavaScript(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node (it runs the generated JavaScript)")
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

	// THE GO ORACLE reads the mutant as damaged.
	m := tabletext.NewModel(u)
	var rep tabletext.Report
	if _, err := tablewire.DecodeRegion(m, m.New(m.Lookup("SimState")), mutant, &rep); err != nil || !rep.Malformed {
		t.Fatalf("the oracle does not read the kind #391 mutant as damaged: %v %+v", err, rep)
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
import { SimStateLoad, TableVariableReport } from "./ScalardemoVariable.js";
const hex = "%s";
const bytes = new Uint8Array(hex.length / 2);
for (let i = 0; i < bytes.length; i++) { bytes[i] = parseInt(hex.substr(i * 2, 2), 16); }
const report = new TableVariableReport();
const v = SimStateLoad(bytes, report);
console.log(JSON.stringify({ malformed: report.malformed, loaded: !!v }));
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
		Malformed bool `json:"malformed"`
	}
	if err := json.Unmarshal(probeOut, &res); err != nil {
		t.Fatalf("could not parse probe output %q: %v", string(probeOut), err)
	}
	if !res.Malformed {
		t.Fatalf("the JS leg did not refuse a damaged load: %s", probeOut)
	}
}
