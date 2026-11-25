package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// plantAdapter runs negative controls: a plant's inputs are {"target":
// <full name>, "env": {...}}, and the plant passes only when its target,
// run with that environment (which breaks the build the target checks), runs
// and its adapter reports it failed. A target that passes, is MISSING, or
// whose adapter crashes or times out fails the plant.
type plantAdapter struct{ rn *testreg.Runner }

type plantInputs struct {
	Target string            `json:"target"`
	Env    map[string]string `json:"env"`
}

func (plantAdapter) Answers(t testreg.Test) bool {
	return t.Scope == testreg.LangFree && t.Type == "plant"
}

func (a plantAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range instances {
		var in plantInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return out, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		target, err := a.rn.Reg.Resolve(in.Target)
		if err != nil {
			return out, fmt.Errorf("%s: %w", inst.Full, err)
		}
		if target.Test.Type == "plant" || len(in.Env) == 0 {
			return out, fmt.Errorf("%s: a plant's target is a test that is not a plant, and its env breaks something", inst.Full)
		}
		rep := a.rn.Run(testreg.WithEnv(ctx, in.Env), []testreg.Instance{target})
		var o testreg.Outcome
		for _, x := range rep.Outcomes {
			if x.Full == target.Full {
				o = x
			}
		}
		if o.Status == testreg.StatusFail && rep.Ran == 1 && len(rep.Problems) == 0 {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
			continue
		}
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
			Reason: fmt.Sprintf("planted %v, %s was %s (ran %d), not red: %s", in.Env, target.Full, o.Status, rep.Ran, o.Reason)})
	}
	return out, nil
}
