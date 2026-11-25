package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/internal/testreg/extract"
)

// A per-language test whose check each language runs as its own make gate
// (a leg's hostile-corpus run, its nightly fuzz campaign, its strict lint,
// its resource bounds) names, in its inputs, each language's make gates:
//
//	{"make": {"c": [{"file": "make/checks/x.mk", "target": "schema-c-x", "vars": {...}}],
//	          "lua": [...]}}
//
// The language's adapter (corpus) runs them through the make adapter, in
// order, and the instance passes only when every one exits 0 with no skip in
// its log. A gate of make/negative-controls.json also names its "group"; it
// runs with no -j, as the controls job ran it, and holds its own gate red.
// A language the row names no gate for FAILS the instance by name (there is
// no owed list: Load refuses one), red in the nightly until the row gives it
// a gate. That is the coverage the old per-language workflows hid: a
// language that never ran a check shows it red here.
type langMakeGate struct {
	makeInputs
	Group string `json:"group,omitempty"`
}

// langMakeGates reads a test's per-language make gates, if it has any.
func langMakeGates(t testreg.Test) (map[string][]langMakeGate, bool, error) {
	if _, ok, _ := extract.LangMakeGates(t.Inputs); !ok || !t.PerLang() {
		return nil, false, nil
	}
	var in struct {
		Make map[string][]json.RawMessage `json:"make"`
	}
	d := json.NewDecoder(bytes.NewReader(t.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return nil, true, fmt.Errorf("per-language make inputs: %w", err)
	}
	if len(in.Make) == 0 {
		return nil, true, fmt.Errorf("per-language make inputs name no language")
	}
	out := map[string][]langMakeGate{}
	for lang, gates := range in.Make {
		if len(gates) == 0 {
			return nil, true, fmt.Errorf("language %s names no make gate", lang)
		}
		for _, raw := range gates {
			var g struct {
				Group string `json:"group"`
			}
			var rest map[string]json.RawMessage
			if err := json.Unmarshal(raw, &rest); err != nil {
				return nil, true, fmt.Errorf("language %s: %w", lang, err)
			}
			if v, ok := rest["group"]; ok {
				if err := json.Unmarshal(v, &g.Group); err != nil || g.Group == "" {
					return nil, true, fmt.Errorf("language %s: a group names a group of make/negative-controls.json", lang)
				}
				delete(rest, "group")
			}
			plain, err := json.Marshal(rest)
			if err != nil {
				return nil, true, err
			}
			mi, err := decodeMakeInputs(plain)
			if err != nil {
				return nil, true, fmt.Errorf("language %s: %w", lang, err)
			}
			out[lang] = append(out[lang], langMakeGate{makeInputs: mi, Group: g.Group})
		}
	}
	return out, true, nil
}

// runLangMake answers per-language make instances for one language.
func runLangMake(ctx context.Context, mk, controls makeAdapter, lang string, instances []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range instances {
		gates, _, err := langMakeGates(inst.Test)
		if err != nil {
			return out, fmt.Errorf("%s: %w", inst.Full, err)
		}
		mine, ok := gates[lang]
		if !ok {
			names := make([]string, 0, len(gates))
			for l := range gates {
				names = append(names, l)
			}
			slices.Sort(names)
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("the row names no make gate for %s (only %s): %s fails this test until the row gives it a gate", lang, strings.Join(names, ", "), lang)})
			continue
		}
		var reasons []string
		for i, g := range mine {
			a := mk
			if g.Group != "" {
				a = controls
			}
			raw, err := json.Marshal(g.makeInputs)
			if err != nil {
				return out, err
			}
			sub := testreg.Instance{Full: fmt.Sprintf("%s#%d", inst.Full, i+1), Lang: testreg.LangFree, Test: testreg.Test{Key: inst.Test.Key, Inputs: raw, Scope: testreg.LangFree}}
			res, err := a.Run(ctx, testreg.LangFree, []testreg.Instance{sub})
			if err != nil {
				return out, fmt.Errorf("%s: %w", inst.Full, err)
			}
			if len(res) != 1 || res[0].Instance != sub.Full {
				return out, fmt.Errorf("%s: the make adapter answered %d results for one gate", inst.Full, len(res))
			}
			if res[0].Verdict != testreg.Pass {
				reasons = append(reasons, res[0].Reason)
			}
		}
		if len(reasons) > 0 {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: strings.Join(reasons, "\n")})
			continue
		}
		out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
	}
	return out, nil
}
