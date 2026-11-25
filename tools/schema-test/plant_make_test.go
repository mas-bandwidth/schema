package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAMakeControlCarriesItsNeeds: a make control's vars follow its target
// on make's command line as a make row's do (DART=dart, so the control does
// not fall back to a default the runner lacks), and a tool it needs that is
// not on PATH fails it before make runs, naming the runner. Its vars, tools
// and siblings are held to the make row's rules.
func TestAMakeControlCarriesItsNeeds(t *testing.T) {
	t.Parallel()
	ctl := func(in string) testreg.Instance {
		return testreg.Instance{Full: "none.functional.plant.g.gate-check", Lang: testreg.LangFree,
			Test: testreg.Test{Scope: testreg.LangFree, Type: "plant", Adapter: "plant", Inputs: json.RawMessage(in)}}
	}
	var ran []string
	a := plantAdapter{make: makeAdapter{
		read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
		exec: func(_ context.Context, av []string) (string, error) {
			ran = append(ran, strings.Join(av, " "))
			return "", nil
		},
		plan: emptyPlan,
		look: func(name string) (string, error) {
			if name == "dart" {
				return "/sdk/bin/dart", nil
			}
			return "", errors.New("not found")
		},
		stat: os.Stat,
		host: func() (string, error) { return "bench-1", nil },
	}}
	res, err := a.Run(context.Background(), "", []testreg.Instance{ctl(`{"make":"gate-check","file":"make/x.mk","group":"g","vars":{"DART":"dart"},"tools":["dart"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Verdict != testreg.Pass {
		t.Fatalf("control with its tool: %+v", res)
	}
	if want := "make --no-print-directory gate-check DART=dart"; len(ran) != 1 || ran[0] != want {
		t.Fatalf("ran %q, want %q", ran, want)
	}
	ran = nil
	res, err = a.Run(context.Background(), "", []testreg.Instance{ctl(`{"make":"gate-check","file":"make/x.mk","group":"g","tools":["luacheck"]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Verdict != testreg.Fail || !strings.Contains(res[0].Reason, "bench-1") || !strings.Contains(res[0].Reason, "luacheck") {
		t.Fatalf("control lacking its tool: %+v", res)
	}
	if len(ran) != 0 {
		t.Fatalf("a control lacking its tool ran make: %q", ran)
	}
	for in, ok := range map[string]bool{
		`{"make":"x-negative-control","file":"Makefile","group":"g","vars":{"DART":"dart"}}`:      true,
		`{"make":"x-negative-control","file":"Makefile","group":"g","vars":{"MAKEFLAGS":"-i"}}`:   false,
		`{"make":"x-negative-control","file":"Makefile","group":"g","vars":{"dart":"x"}}`:         false,
		`{"make":"x-negative-control","file":"Makefile","group":"g","tools":["/usr/bin/dart"]}`:   false,
		`{"make":"x-negative-control","file":"Makefile","group":"g","siblings":["serialize.go"]}`: false,
		`{"vars":{"DART":"dart"}}`: false,
		`{"target":"go.functional.roundtrip.fam.one","env":{"B":"1"},"reason":"a stated reason, long","vars":{"DART":"dart"}}`: false,
	} {
		test := testreg.Test{Scope: testreg.LangFree, Type: "plant", Inputs: json.RawMessage(in)}
		if got := (plantAdapter{}).Answers(test); got != ok {
			t.Errorf("%s: answered %v, want %v", in, got, ok)
		}
	}
}
