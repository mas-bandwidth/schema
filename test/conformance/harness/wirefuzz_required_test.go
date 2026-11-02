package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Required fields are semantic refusals, not malformed wire or oracle failures.
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
					if answer.required != missing {
						t.Fatalf("required refusal=%t want %t: %+v", answer.required, missing, answer)
					}
					if !missing {
						if answer.report != (Counts{}) || answer.encFail {
							t.Fatalf("clean required value refused: %+v", answer)
						}
						return
					}
					for _, loaded := range []bool{false, true} {
						good := legReply{loaded: loaded, report: Counts{Refused: true}, saveFail: true}
						if verdict := wireVerdict(root, good, answer, len(wire)); verdict != "" {
							t.Fatal(verdict)
						}
						for _, bad := range []Counts{{}, {Malformed: true}, {Refused: true, Malformed: true}} {
							good.report = bad
							if verdict := wireVerdict(root, good, answer, len(wire)); verdict == "" {
								t.Fatalf("accepted required refusal with report %+v", bad)
							}
						}
					}
				})
			}
		}
	}
}
