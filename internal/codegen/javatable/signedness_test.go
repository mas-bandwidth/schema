package javatable

import (
	"strings"
	"testing"
)

func TestJavaTableEmitsCheckerSignednessOnIntegerStorage(t *testing.T) {
	t.Parallel()

	files, err := Generate(unitFrom(t, `package signednessprobe

fixed table Probe
{
    signed int32
    unsigned uint32
    marks [2]uint16
}
`))
	if err != nil {
		t.Fatal(err)
	}
	fixed := string(files["ProbeFixed.java"])
	for _, want := range []string{
		"public @org.checkerframework.checker.signedness.qual.Signed int signed;",
		"public @org.checkerframework.checker.signedness.qual.Unsigned int unsigned;",
		"public final @org.checkerframework.checker.signedness.qual.Unsigned short[] marks",
	} {
		if !strings.Contains(fixed, want) {
			t.Errorf("generated fixed form missing %q:\n%s", want, fixed)
		}
	}
	row := string(files["ProbeRow.java"])
	for _, want := range []string{
		"public static @org.checkerframework.checker.signedness.qual.Signed int signed(",
		"public static @org.checkerframework.checker.signedness.qual.Unsigned int unsigned(",
		"public static @org.checkerframework.checker.signedness.qual.Unsigned short marks(",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("generated row missing %q:\n%s", want, row)
		}
	}
}
