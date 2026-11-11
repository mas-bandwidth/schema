package cstable

// EMITTED READ RULES: unit checks over the REAL emitted artifact (no
// interpreter, socket or network). Generate runs over a genuine schema, and
// each rule names the method it lives in, by the two signatures that bound
// that method in the emitted runtime, and the line that holds the rule. The
// negative control takes each line out of that same artifact, one at a time,
// and requires the check red, so no line is held hollow.

import (
	"fmt"
	"strings"
	"testing"
)

// csEmittedRule is one line the emitted reader must carry inside one method.
type csEmittedRule struct {
	start, end string // the signatures bounding the method
	line       string // the rule's emitted line
	why        string // what is lost without it
}

// csEmittedRulesHold answers the first rule the artifact does not carry.
func csEmittedRulesHold(files map[string][]byte, rules []csEmittedRule) error {
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	src := b.String()
	for _, r := range rules {
		body, err := csWireFuzzSlice(src, r.start, r.end)
		if err != nil {
			return err
		}
		if !strings.Contains(body, r.line) {
			return fmt.Errorf("%s: the emitted %q carries no %q", r.why, r.start, r.line)
		}
	}
	return nil
}

// csEmittedRulesTest holds the artifact to the rules, then takes each rule's
// line out in turn and requires the check red.
func csEmittedRulesTest(t *testing.T, schema string, rules []csEmittedRule) {
	t.Helper()
	files := generateFiles(t, "Probe", schema)
	if err := csEmittedRulesHold(files, rules); err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		sabotaged := map[string][]byte{}
		planted := 0
		for name, data := range files {
			s := string(data)
			planted += strings.Count(s, r.line)
			sabotaged[name] = []byte(strings.ReplaceAll(s, r.line, "/* SABOTAGED */"))
		}
		if planted == 0 {
			t.Fatalf("NEGATIVE CONTROL FAILED: %q is not in the artifact to take out", r.line)
		}
		if err := csEmittedRulesHold(sabotaged, rules); err == nil {
			t.Fatalf("NEGATIVE CONTROL FAILED: taking %q out of the artifact left the check GREEN (%s)", r.line, r.why)
		}
	}
}
