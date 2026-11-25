// THE MAIN SWEEP AS REGISTRY ROWS: the bit-phase family. Every row of the
// required roster (requiredRoster: the 0-bit EmptyZero pair, and for each
// phase three valid variants and five hostile inputs) is one registry test,
// <roundtrip|load-refusal>.bit-phase.<file>, which every language answers
// through its leg's driver over the corpus the C++ oracle generates from
// test/verify-bitphase/BitPhase.schema (cpp/oracle.cpp). The main corpus's
// own steps are two more tests: decode.bit-phase.cfdecode (the compressed
// float read at every index, cfdecode.go) and roundtrip.bit-phase.f32lit (the
// float32 literal case, f32lit.go).
//
// --adapter answers them (adapter.go): the requested vector rows run as the
// align-arrays rows do, all together and, when that is red, each alone, with
// the main-only steps left out; each main-only step's verdict is a run of
// the leg with its steps in, over the whole roster, and is that
// step's check of the output, so a red decode never reads as a red vector.
//
// `verify-bitphase --bit-phase-rows` prints the rows;
// TestTheRegistryHoldsTheBitPhaseRows holds the registry to them.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// BitPhaseFamily is the main sweep's family in the registry.
const BitPhaseFamily = "bit-phase"

// BitPhaseSchema is the main sweep's schema, from the repository root.
const BitPhaseSchema = "test/verify-bitphase/BitPhase.schema"

// bitPhaseDriver is every language's driver as a row names it: the C++
// leg's is the committed directory cpp/ (oracle.cpp, which also generates
// the corpus, and verify.cpp); every other leg's is written by
// drivers_compiled.go or drivers_vm.go into the run's work folder.
const bitPhaseDriver = "test/verify-bitphase/{lang}/driver"

// The main-only steps' cases.
const (
	bitPhaseCfDecodeCase = "cfdecode"
	bitPhaseF32LitCase   = "f32lit"
)

// bitPhaseInputs is a bit-phase row's inputs: a vector row names its root,
// kind and file; a step row names its step. Every row names the schema it
// reads and its driver.
type bitPhaseInputs struct {
	Schema string   `json:"schema"`
	Root   string   `json:"root,omitempty"`
	Kind   string   `json:"kind,omitempty"`
	File   string   `json:"file,omitempty"`
	Step   string   `json:"step,omitempty"`
	Files  []string `json:"files"`
	Driver string   `json:"driver"`
}

type bitPhaseRow struct {
	Rec    string         `json:"rec"`
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Family string         `json:"family"`
	Case   string         `json:"case"`
	Tier   string         `json:"tier"`
	Checks string         `json:"checks"`
	Inputs bitPhaseInputs `json:"inputs"`
	Expect string         `json:"expect"`
	Source string         `json:"source"`
}

// bitPhaseCase is a roster file as a registry case.
func bitPhaseCase(file string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSuffix(file, ".bin"), "_", "-"), ".", "-")
}

// bitPhaseRows is every row: the roster's, in roster order, then the steps.
func bitPhaseRows() []bitPhaseRow {
	in := func() bitPhaseInputs {
		return bitPhaseInputs{Schema: BitPhaseSchema, Files: []string{BitPhaseSchema}, Driver: bitPhaseDriver}
	}
	var out []bitPhaseRow
	for _, c := range requiredRoster() {
		i := in()
		i.Root, i.Kind, i.File = c.Type, c.Kind, c.File
		r := bitPhaseRow{Rec: "test", Family: BitPhaseFamily, Case: bitPhaseCase(c.File), Tier: "functional", Inputs: i,
			Source: "test/verify-bitphase/main.go requiredRoster"}
		if c.Kind == "valid" {
			r.Type = "roundtrip"
			r.Checks = fmt.Sprintf("bit-phase sweep %s %s: the oracle's valid wire is read and written back to the same bytes", c.Type, c.File)
			r.Expect = "the leg reads the wire and writes back exactly its bytes"
		} else {
			r.Type = "load-refusal"
			r.Checks = fmt.Sprintf("bit-phase sweep %s %s: the oracle's hostile wire is refused", c.Type, c.File)
			r.Expect = "the leg refuses the wire"
		}
		r.Key = r.Type + "." + BitPhaseFamily + "." + r.Case
		out = append(out, r)
	}
	cf := in()
	cf.Step = bitPhaseCfDecodeCase
	out = append(out, bitPhaseRow{Rec: "test", Key: "decode." + BitPhaseFamily + "." + bitPhaseCfDecodeCase, Type: "decode", Family: BitPhaseFamily,
		Case: bitPhaseCfDecodeCase, Tier: "functional", Inputs: cf, Source: "test/verify-bitphase/cfdecode.go",
		Checks: fmt.Sprintf("the compressed float CfDecode is read at every one of its %d indices, in every tier the leg carries", cfDecodeSteps+1),
		Expect: "every index's float32 is the decode SPEC §4.6 gives"})
	fl := in()
	fl.Step = bitPhaseF32LitCase
	out = append(out, bitPhaseRow{Rec: "test", Key: "roundtrip." + BitPhaseFamily + "." + bitPhaseF32LitCase, Type: "roundtrip", Family: BitPhaseFamily,
		Case: bitPhaseF32LitCase, Tier: "functional", Inputs: fl, Source: "test/verify-bitphase/f32lit.go",
		Checks: "the float32 literal case: F32Const, F32Lit's defaults, and both float32 ends written",
		Expect: "every probe's answer is the float32 the schema's literal names, in every tier the leg carries"})
	return out
}

func writeBitPhaseRows(w io.Writer) error {
	for _, r := range bitPhaseRows() {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, string(b)); err != nil {
			return err
		}
	}
	return nil
}

// bitPhaseRowsByCase is the rows by their registry case.
func bitPhaseRowsByCase() map[string]bitPhaseRow {
	out := map[string]bitPhaseRow{}
	for _, r := range bitPhaseRows() {
		out[r.Case] = r
	}
	return out
}
