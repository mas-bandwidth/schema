package javatable

import (
	"strings"
	"testing"
)

func TestJavaTableEmitsCheckerSignednessOnIntegerStorage(t *testing.T) {
	t.Parallel()

	u := unitFrom(t, `package signednessprobe

table VarProbe
{
    signedVal int32 = 1
    unsignedVal uint32 = 2
    listVals [0..4]uint32
}

fixed table Probe
{
    signedVal int32
    unsignedVal uint32
    marks [2]uint16
}
`)
	plain, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain["ProbeFixed.java"]), "checkerframework") {
		t.Fatalf("default Generate emitted Checker Framework qualifiers:\n%s", plain["ProbeFixed.java"])
	}
	files, err := GenerateWithCheckerQual(u)
	if err != nil {
		t.Fatal(err)
	}
	fixed := string(files["ProbeFixed.java"])
	for _, want := range []string{
		"public @org.checkerframework.checker.signedness.qual.Signed int signedVal;",
		"public @org.checkerframework.checker.signedness.qual.Unsigned int unsignedVal;",
		"public final @org.checkerframework.checker.signedness.qual.Unsigned short[] marks",
	} {
		if !strings.Contains(fixed, want) {
			t.Errorf("generated fixed form missing %q:\n%s", want, fixed)
		}
	}
	row := string(files["ProbeRow.java"])
	for _, want := range []string{
		"public static @org.checkerframework.checker.signedness.qual.Signed int signedVal(",
		"public static @org.checkerframework.checker.signedness.qual.Unsigned int unsignedVal(",
		"public static @org.checkerframework.checker.signedness.qual.Unsigned short marks(",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("generated row missing %q:\n%s", want, row)
		}
	}
	variable := string(files["VarProbeVariable.java"])
	for _, want := range []string{
		"public @org.checkerframework.checker.signedness.qual.Signed int signedVal = 1;",
		"public @org.checkerframework.checker.signedness.qual.Unsigned int unsignedVal = 2;",
		"public List<Integer> listVals = new ArrayList<>();",
	} {
		if !strings.Contains(variable, want) {
			t.Errorf("generated variable form missing %q:\n%s", want, variable)
		}
	}
	if strings.Contains(variable, "List<Integer>") && strings.Contains(variable, ".Unsigned List<") {
		t.Errorf("generated variable form annotated boxed generic List<Integer>:\n%s", variable)
	}
}
