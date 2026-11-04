package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDerivedFramingWithholdsAdmissionAndValueHints(t *testing.T) {
	manifest := &Manifest{Framings: []FramingCase{
		{Name: "accept", Unit: "u", Root: "R", Wire: "a.bin", Value: "secret-value.json"},
		{Name: "reject", Unit: "u", Root: "R", Wire: "b.bin", Refused: true},
	}}
	path := filepath.Join(t.TempDir(), "manifest")
	if err := deriveManifest(manifest, path); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "framing ") {
			continue
		}
		rows++
		if len(strings.Fields(line)) != 5 {
			t.Fatalf("derived framing exposes an expected outcome: %s", line)
		}
	}
	if rows != 2 || strings.Contains(string(body), "secret-value") {
		t.Fatalf("derived framing input is incomplete or exposes an answer: %s", body)
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

func TestManifestRejectsDuplicateCookRecipes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest")
	body := "unit u unit.schema\ninstance value u Root value.bin\ncook-write value little.cook big.cook\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(path, defaultJSONDir); err != nil {
		t.Fatalf("one recipe: %v", err)
	}
	if err := os.WriteFile(path, []byte(body+"cook-write value other.cook other-be.cook\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(path, defaultJSONDir); err == nil || !strings.Contains(err.Error(), "duplicate cook-write") {
		t.Fatalf("duplicate recipes can append or overwrite one answer: %v", err)
	}
}
