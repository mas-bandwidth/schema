package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Missing required fields refuse the whole read, independent of diagnostics.
// Use the committed independently framed corpus on mutable, region and retaining paths.
func TestWireFuzzRequiredRefusal(t *testing.T) {
	dir := "../../../testdata/conformance/feature-witnesses/required"
	m := &Manifest{Units: []Unit{{Key: "required", Paths: []string{filepath.Join(dir, "Probe.schema")}}}}
	for _, retain := range []bool{false, true} {
		for _, builder := range []bool{false, true} {
			root, err := newWireRoot(newUnits(m), "required", "Root", false, retain)
			if err != nil {
				t.Fatal(err)
			}
			root.builder = builder
			for _, name := range []string{"missing", "missing-b", "zero", "populated"} {
				t.Run(name+map[bool]string{false: "/region", true: "/builder"}[builder]+map[bool]string{false: "", true: "/retain"}[retain], func(t *testing.T) {
					wire, err := os.ReadFile(filepath.Join(dir, name+".bin"))
					if err != nil {
						t.Fatal(err)
					}
					answer, err := root.oracle(wire)
					if err != nil {
						t.Fatal(err)
					}
					missing := strings.HasPrefix(name, "missing")
					if answer.accepted == missing {
						t.Fatalf("accepted=%t missing=%t", answer.accepted, missing)
					}
					if !missing {
						if len(answer.encoded) == 0 {
							t.Fatal("accepted value has no write-back")
						}
						return
					}
					if len(answer.encoded) != 0 {
						t.Fatal("missing required fields produced default bytes")
					}
					for _, good := range []legReply{{loaded: false, report: Counts{Refused: true}}, {loaded: true, report: Counts{Refused: true}}, {loaded: true, report: Counts{Malformed: true}}} {
						if verdict := wireVerdict(root, good, answer, len(wire)); verdict != "" {
							t.Fatal(verdict)
						}
					}
					if verdict := wireVerdict(root, legReply{loaded: true}, answer, len(wire)); verdict == "" {
						t.Fatal("accepted missing required fields")
					}
				})
			}
		}
	}
}
