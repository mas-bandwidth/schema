package main

import (
	"errors"
	"strings"
	"testing"
)

// TestAnEmptySampleIsRed is F-045: `verify-tablegen --lang js --schemas 0`
// printed TABLEGEN_OK js 0 and exited 0, where --schemas 1 runs 25 cases. The
// sample and every driver's case count are held above zero in the two places
// all 18 targets pass through.
func TestAnEmptySampleIsRed(t *testing.T) {
	pair := func(i int, v1, v2 string) PairMeta { return PairMeta{Index: i, Name: v1, PkgV1: v1, PkgV2: v2} }
	meta := &CorpusMeta{Schemas: 2, CaseCount: 3, Pairs: []PairMeta{pair(0, "a1", "a2"), pair(1, "b1", "b2")}}
	cases := []CorpusCase{{ID: "c0", Pkg: "a1"}, {ID: "c1", Pkg: "a2"}, {ID: "c2", Pkg: "b1"}}
	if err := proveSample(2, meta, cases); err != nil {
		t.Fatalf("CONTROL FAILED: a sample of two pairs and three cases: %v", err)
	}
	for _, tc := range []struct {
		name    string
		schemas int
		meta    *CorpusMeta
		cases   []CorpusCase
		says    string
	}{
		{"--schemas 0", 0, &CorpusMeta{}, nil, "--schemas 0: the sample is empty"},
		{"a negative sample", -1, &CorpusMeta{}, nil, "the sample is empty"},
		{"fewer pairs than asked", 3, meta, cases, "carries 2 schema pair(s) and --schemas asked for 3"},
		{"no case", 2, &CorpusMeta{Schemas: 2, Pairs: meta.Pairs}, nil, "a run over no case passes nothing"},
		{"a case count the meta does not state", 2, meta, cases[:2], "carries 2 case(s) and its meta says 3"},
		{"a pair with no case", 2, meta, []CorpusCase{{ID: "c0", Pkg: "a1"}, {ID: "c1", Pkg: "a2"}, {ID: "c2", Pkg: "a1"}}, "schema pair 1 (b1) contributes no case"},
		{"a case twice", 2, meta, []CorpusCase{{ID: "c0", Pkg: "a1"}, {ID: "c0", Pkg: "a2"}, {ID: "c2", Pkg: "b1"}}, "carries the case c0 twice"},
	} {
		if err := proveSample(tc.schemas, tc.meta, tc.cases); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: want red saying %q, got %v", tc.name, tc.says, err)
		}
	}

	// every target's driver output passes through requireToken
	for _, lang := range allLangs {
		if err := requireToken("TABLEGEN_OK "+lang+" 25\n", nil, lang, 25); err != nil {
			t.Errorf("CONTROL FAILED: %s over 25 cases: %v", lang, err)
		}
		if err := requireToken("TABLEGEN_OK "+lang+" 0\n", nil, lang, 0); err == nil || !strings.Contains(err.Error(), "a driver over no case proves nothing") {
			t.Errorf("%s: TABLEGEN_OK over no case passed: %v", lang, err)
		}
		if err := requireToken("TABLEGEN_OK "+lang+" 24\n", nil, lang, 25); err == nil {
			t.Errorf("%s: a driver one case short passed", lang)
		}
		if err := requireToken("", errors.New("exit 1"), lang, 25); err == nil {
			t.Errorf("%s: a failed driver passed", lang)
		}
	}
	if len(allLangs) != 18 {
		t.Errorf("the tablegen roster names %d targets, not 18: %v", len(allLangs), allLangs)
	}
}
