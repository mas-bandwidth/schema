package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSingleReadProtocolIgnoresDiagnosticsButKeepsValues(t *testing.T) {
	dir := t.TempDir()
	value := filepath.Join(dir, "value.json")
	if err := os.WriteFile(value, []byte("{\"n\":7}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, answer string
		wantPass     bool
	}{
		{"one bit", "read\n{\"n\":7}\n", true},
		{"diagnostics", "17,4,91,3,2,false,read\n{\"n\":7}\n", true},
		{"changed value", "17,4,91,3,2,false,read\n{\"n\":8}\n", false},
		{"missing value", "read\n", false},
		{"wrong refusal", "refused\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "row"), []byte(tc.answer), 0o644); err != nil {
				t.Fatal(err)
			}
			var result result
			scoreAnswer("probe", "report", dir, expectation{name: "row", want: []byte("read\n"), value: value}, Instance{}, &result)
			if (result.pass == 1) != tc.wantPass {
				t.Fatalf("pass=%d, failures=%v", result.pass, result.failures)
			}
		})
	}
}

func TestSingleReadProtocolDoesNotReclassifyRecovery(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, answer string
		wantPass     bool
	}{
		{"refused", "REFUSED\n", true},
		{"legacy damage", "99,0,0,0,0,true,read\npartial value", true},
		{"recovering read", "0,1,0,1,1,false,read\npartial value", false},
		{"clean read", "read\n{}\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "row"), []byte(tc.answer), 0o644); err != nil {
				t.Fatal(err)
			}
			var result result
			scoreAnswer("probe", "framing", dir, expectation{name: "row", want: []byte("refused\n")}, Instance{}, &result)
			if (result.pass == 1) != tc.wantPass {
				t.Fatalf("pass=%d, failures=%v", result.pass, result.failures)
			}
		})
	}
}

func TestSingleReadProtocolHasNoAcceptedValueExemption(t *testing.T) {
	h := Hostile{Name: "row", Refused: true, CarriedBy: []string{"go"}, Uncarried: "0,0,0,1,0,false,read"}
	e := hostileExpectation(h, "probe")
	if string(e.want) != "refused\n" || e.verdictOnly || e.value != "" {
		t.Fatalf("old recovery was accepted as a second standard: %+v", e)
	}
}
