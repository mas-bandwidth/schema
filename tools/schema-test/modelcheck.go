package main

import (
	"context"
	"io"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// modelCheckAdapter runs the TLA+ model checks: a language-free row of type
// model whose inputs are {"config": "MC<x>.cfg", "workers": n} is one TLC
// run over tla/<config>, judged against its tla/RUNS.tsv row (an exhaustive
// run explores the recorded distinct states with no error; a reversed
// witness violates exactly the invariant its row names); a row whose inputs
// are {"controls": true} proves that judge refuses three broken trees.
// tools/tlcrun -adapter is the harness. A runner without the TLC jar or java
// fails every instance naming the runner.
type modelCheckAdapter struct {
	build *builder
	log   io.Writer
}

// tlcrunBin is the harness, built once per run.
const tlcrunBin = "build/tlcrun"

func (modelCheckAdapter) Answers(t testreg.Test) bool {
	return t.Scope == testreg.LangFree && (t.Type == "model" || t.Type == "plant" && t.Adapter == "model-check")
}

func (a modelCheckAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	if err := a.build.run(ctx, []string{"go", "build", "-o", tlcrunBin, "./tools/tlcrun"}); err != nil {
		return nil, err
	}
	return runHarness(ctx, []string{"./" + tlcrunBin, "-adapter", "-tla", "tla"}, instances, a.log)
}
