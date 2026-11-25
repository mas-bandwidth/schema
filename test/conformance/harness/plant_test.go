package main

import (
	"bytes"
	"testing"
)

func plantFixture() map[string][]expectation {
	return map[string][]expectation{
		"report": {
			{name: "fixedevo_v1_as_v3", want: []byte("read\n"), value: "v"},
			{name: "a1_arm_body_as_a2", want: []byte("refused\n"), damaged: true},
		},
		"retain-save": {{name: "retain_node_record", want: []byte{1, 2}}},
	}
}

// Each `run` plant makes exactly its own case's expectation wrong.
func TestPlantExpectationsMakesOneCaseWrong(t *testing.T) {
	// t.Setenv: not parallel
	t.Setenv(plantEnv, "fixed")
	w := plantFixture()
	if err := plantExpectations(w); err != nil {
		t.Fatal(err)
	}
	if e := w["report"][0]; string(e.want) != "refused\n" || !e.damaged || e.value != "" {
		t.Errorf("fixed plant left %+v", e)
	}
	if e := w["report"][1]; string(e.want) != "refused\n" {
		t.Errorf("fixed plant moved another case: %+v", e)
	}

	t.Setenv(plantEnv, "variable")
	w = plantFixture()
	if err := plantExpectations(w); err != nil {
		t.Fatal(err)
	}
	if e := w["report"][1]; string(e.want) != "read\n" || e.damaged {
		t.Errorf("variable plant left %+v", e)
	}

	t.Setenv(plantEnv, "retain")
	w = plantFixture()
	if err := plantExpectations(w); err != nil {
		t.Fatal(err)
	}
	if e := w["retain-save"][0]; !bytes.Equal(e.want, []byte{1, 0xfd}) {
		t.Errorf("retain plant left %x", e.want)
	}

	t.Setenv(plantEnv, "")
	w = plantFixture()
	if err := plantExpectations(w); err != nil || string(w["report"][0].want) != "read\n" {
		t.Errorf("an unset plant changed something: %v", err)
	}
}

// A class nobody knows is an error; a manifest without the case is left
// unplanted (said on stderr), and changes nothing.
func TestPlantExpectationsRefusesAnUnknownClass(t *testing.T) {
	t.Setenv(plantEnv, "fixed")
	w := map[string][]expectation{"report": {{name: "other", want: []byte("read\n")}}}
	if err := plantExpectations(w); err != nil || string(w["report"][0].want) != "read\n" {
		t.Errorf("a manifest without the case: %v, %+v", err, w)
	}
	t.Setenv(plantEnv, "nonsense")
	if err := plantExpectations(plantFixture()); err == nil {
		t.Error("an unknown plant was accepted")
	}
}

func TestPlantMessageAnswerInverts(t *testing.T) {
	t.Parallel()
	if a := plantMessageAnswer(oracleAnswer{accepted: true}); a.accepted || !a.report.Refused {
		t.Errorf("accepted answer planted as %+v", a)
	}
	if a := plantMessageAnswer(oracleAnswer{report: Counts{Refused: true}}); !a.accepted {
		t.Errorf("refused answer planted as %+v", a)
	}
}
