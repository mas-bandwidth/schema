package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSchemaJSEngineDifferentialPlantedBigInt is the gate over the JavaScript
// engine differential's blade (make/schema-js-engine-differential.mk). The
// differential runs the port's own conformance driver under Node, Deno and a
// headless browser and requires every per-case answer to be identical; this
// test proves the CONFORMANCE comparison catches the language mistake the
// differential exists for: an int64 that crosses Number() instead of BigInt.
//
// It shells out to make and to node, so it is a functional test behind the
// slow gate. It uses the generated tree the js leg's own targets already
// build; no network, no sockets and no real clock.
//
// The red line this test keeps: at BASE the target did not exist, so the test
// failed by name. With the target, the plant (BigInt.asIntN(64, ->
// Number() over a scratch copy) must turn the run red, the control target must
// print the plant it applied, and the same target with no plant must stay
// green. A control that goes green with the plant in is a control over
// nothing.
func TestSchemaJSEngineDifferentialPlantedBigInt(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make, node and the generated JavaScript conformance tree")

	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("the differential gate shells out to make: %v", err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("the differential gate needs node: %v", err)
	}

	root := repoRoot(t)
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("make", append([]string{"--no-print-directory"}, args...)...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// GREEN WITH THE PLANT OUT. The plain run is the same comparison the
	// nightly gate makes, with the corpus's expectations holding every case.
	out, err := run("schema-js-engine-differential-node")
	if err != nil {
		t.Fatalf("the differential was red with no plant:\n%s", out)
	}
	if !strings.Contains(out, "node agrees with the conformance corpus") {
		t.Fatalf("the differential did not report its green line:\n%s", out)
	}

	// RED WITH THE PLANT IN, and the control names what it planted.
	out, err = run("schema-js-engine-differential-negative-control")
	if err != nil {
		t.Fatalf("the negative control itself failed:\n%s", out)
	}
	if !strings.Contains(out, "BigInt.asIntN(64, replaced by Number()") {
		t.Fatalf("the control did not name the plant:\n%s", out)
	}

	log, err := os.ReadFile(filepath.Join(root, "build", "js-engine-differential", "plant.log"))
	if err != nil {
		t.Fatalf("the planted run wrote no log: %v", err)
	}
	if !strings.Contains(string(log), "FAILURES") || !strings.Contains(string(log), "js / wire") {
		t.Fatalf("the planted BigInt->Number tree did not red the conformance comparison:\n%s", log)
	}
}
