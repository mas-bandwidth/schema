package darttable

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// A FIXED ROOT IS FORM 3 ON THE TABLE WIRE, BOTH WAYS
// (docs/SPEC-TABLES.md §3.4; internal/tablewire/fixedroot_test.go).
// TestMapsAndUnionArmsOnTheTableWireDart and TestFixedClassOnTableWireDart
// resave through resaveOracleRows. A root whose artifact contains FixedLoad
// is resaved with FixedLoad/FixedSave, not WireLoad/WireSave. The read is
// one read: the records or a refusal. Note is a variable root the fixed
// form refuses (an unbounded array), so it has no FixedLoad and stays on
// the form-1 wire. A plain table the fixed emitter can carry also has
// FixedLoad; the probe follows the artifact, not the keyword.
//
// This is a unit test. It reads the probe resaveOracleRows writes. It does
// not run Dart, take real time, open a socket, or use the network.
const fixedRootThreeUnit = `package fixedpin
fixed table Root
{
    a    int32
    flag bool
}

table Note
{
    items []int32
}
`

func TestDartFixedRootSavesFormThree(t *testing.T) {
	t.Parallel()
	probe := fixedRootResaveProbe(t)
	if err := fixedRootResaveHolds(probe, "Root"); err != nil {
		t.Fatal(err)
	}
	if err := variableRootStaysOnFormOne(probe, "Note"); err != nil {
		t.Fatal(err)
	}
}

// TestDartFixedRootSavesFormThreeNegativeControl plants the form-1 fault in
// the real resave probe: the fixed root's FixedSave becomes a WireLoad/
// WireSave round trip. The same rule must reject that probe. A control that
// does not touch the probe proves nothing.
func TestDartFixedRootSavesFormThreeNegativeControl(t *testing.T) {
	t.Parallel()
	probe := fixedRootResaveProbe(t)
	if err := fixedRootResaveHolds(probe, "Root"); err != nil {
		t.Fatalf("the real probe does not hold the rule, so the control has nothing to break: %v", err)
	}
	planted, err := plantFormOneResave(probe, "Root")
	if err != nil {
		t.Fatal(err)
	}
	err = fixedRootResaveHolds(planted, "Root")
	if err == nil {
		t.Fatal("negative control passed: a form-1 resave of the fixed root still held the rule")
	}
	if !strings.Contains(err.Error(), "form 1") {
		t.Fatalf("negative control failed for a different reason: %v", err)
	}
}

// fixedRootResaveProbe is the probe resaveOracleRows writes for one fixed
// root that has FixedLoad and one variable root that does not.
func fixedRootResaveProbe(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("mapsarms_test.go")
	if err != nil {
		t.Fatal(err)
	}
	fn := sourceFunc(string(src), "resaveOracleRows")
	if fn == "" || !strings.Contains(fn, "resaveOracleProbe(") {
		t.Fatal("resaveOracleRows does not resave through resaveOracleProbe")
	}
	if strings.Contains(fn, "WireLoad(") || strings.Contains(fn, "WireSave(") {
		t.Fatal("resaveOracleRows resaves with WireLoad/WireSave instead of the probe")
	}
	u := unitFrom(t, fixedRootThreeUnit)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if !artifactHasFixedLoad(files, "Root") {
		t.Fatal("Root has no FixedLoad in the artifact; this test would prove nothing")
	}
	if artifactHasFixedLoad(files, "Note") {
		t.Fatal("variable root Note has FixedLoad; the probe selection would prove nothing")
	}
	wireName := runtimeHome(u) + "Wire.dart"
	if len(files[wireName]) == 0 {
		t.Fatalf("no %s", wireName)
	}
	rows := []oracleRow{
		{name: "root", root: "Root"},
		{name: "note", root: "Note"},
	}
	return resaveOracleProbe(wireName, files, rows)
}

func artifactHasFixedLoad(files map[string][]byte, root string) bool {
	fn := strings.ToLower(root[:1]) + root[1:]
	needle := "int " + fn + "FixedLoad("
	for name, body := range files {
		if strings.HasSuffix(name, "Fixed.dart") && strings.Contains(string(body), needle) {
			return true
		}
	}
	return false
}

func sourceFunc(src, name string) string {
	i := strings.Index(src, "func "+name+"(")
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
		return rest[:1+j]
	}
	return rest
}

// fixedRootResaveHolds is the rule, over the probe resaveOracleRows writes
// and over a copy with a form-1 resave planted in the fixed root's save.
func fixedRootResaveHolds(probe, root string) error {
	arm, ok := probeCase(probe, root)
	if !ok {
		return fmt.Errorf("%s resave is not form 3: no case in the resave probe", root)
	}
	fn := strings.ToLower(root[:1]) + root[1:]
	if strings.Contains(arm, fn+"WireLoad(") || strings.Contains(arm, fn+"WireSave(") {
		return fmt.Errorf("%s resave is form 1: the probe resaves with %sWireLoad/%sWireSave, not FixedLoad/FixedSave", root, fn, fn)
	}
	if !strings.Contains(arm, fn+"FixedLoad(") {
		return fmt.Errorf("%s resave is not form 3: the probe does not call %sFixedLoad", root, fn)
	}
	if !strings.Contains(arm, fn+"FixedSave(") {
		return fmt.Errorf("%s resave is not form 3: the probe does not call %sFixedSave", root, fn)
	}
	// One read: the records (the load must return them) or a refusal. A load
	// whose result is ignored can resave a default.
	if !strings.Contains(arm, "!= 1") || !strings.Contains(arm, "report.refused") {
		return fmt.Errorf("%s resave is not one read: the load does not require the records or a refusal", root)
	}
	return nil
}

func variableRootStaysOnFormOne(probe, root string) error {
	arm, ok := probeCase(probe, root)
	if !ok {
		return fmt.Errorf("variable root %s has no case in the resave probe", root)
	}
	fn := strings.ToLower(root[:1]) + root[1:]
	if !strings.Contains(arm, fn+"WireLoad(") || !strings.Contains(arm, fn+"WireSave(") {
		return fmt.Errorf("variable root %s was moved off the form-1 wire", root)
	}
	if strings.Contains(arm, "FixedLoad(") || strings.Contains(arm, "FixedSave(") {
		return fmt.Errorf("variable root %s rides the fixed codec", root)
	}
	return nil
}

func probeCase(probe, root string) (string, bool) {
	head := "case '" + root + "':"
	i := strings.Index(probe, head)
	if i < 0 {
		return "", false
	}
	rest := probe[i:]
	if j := strings.Index(rest[len(head):], "\n    case '"); j >= 0 {
		return rest[:len(head)+j], true
	}
	if j := strings.Index(rest[len(head):], "\n    default:"); j >= 0 {
		return rest[:len(head)+j], true
	}
	return rest, true
}

// plantFormOneResave turns the fixed root's real form-3 save into the form-1
// resave the probe used to emit for every root.
func plantFormOneResave(probe, root string) (string, error) {
	fn := strings.ToLower(root[:1]) + root[1:]
	save := "f." + fn + "FixedSave(values, 1, out)"
	if strings.Count(probe, save) != 1 {
		return "", fmt.Errorf("could not plant a form-1 resave: %q appears %d times in the probe", save, strings.Count(probe, save))
	}
	form1 := fn + "WireSave(" + fn + "WireLoad(wire))"
	return strings.Replace(probe, save, form1, 1), nil
}
