package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// THE PLANTED ORACLE, for the negative controls of every leg
// (make/widen-18-gate-controls.mk verify-bitphase-negative-control-<lang> and
// verify-sabotage-packet-negative-control-<lang>; 2026-11-24). A gate that
// has never gone red on a leg may be comparing that leg with nothing: the
// emitter sabotage proves it for JavaScript only. Here the ORACLE is made
// wrong instead, the same way for every leg: SCHEMA_GATE_PLANT names a class,
// and the corpus the legs read is a copy in which, for every phase, one valid
// vector and one hostile vector of that class have traded bytes. A leg whose
// gate really compares its verdicts must now refuse a "valid" vector and
// accept a "hostile" one, and go red by name.
//
//	bitphase  the misaligned-bit refusals (hostile_align_p<k>)
//	packet    the reserved-bit refusals (hostile_reserved_p<k>), the packet
//	          class of the sabotage controls
//
// The swap keeps the manifest's roster and keeps every vector's bytes
// distinct, so the corpus still loads; only its answers are wrong.
func plantKind(plant string) (string, bool) {
	switch plant {
	case "bitphase":
		return "align", true
	case "packet":
		return "reserved", true
	}
	return "", false
}

// plantCorpus writes the planted copy of corpusDir under dir, swaps the same
// pairs in blobs, and returns the copy's path and the pairs it swapped.
func plantCorpus(corpusDir, dir, plant string, cases []manifestCase, blobs map[string][]byte) (string, []string, error) {
	kind, ok := plantKind(plant)
	if !ok {
		return "", nil, fmt.Errorf("SCHEMA_GATE_PLANT=%q: bitphase plants bitphase or packet", plant)
	}
	valid := map[string]string{}
	for _, c := range cases {
		if c.Kind == "valid" && valid[c.Type] == "" {
			valid[c.Type] = c.File
		}
	}
	var swapped []string
	for _, c := range cases {
		if c.Kind != "refuse" || !strings.HasPrefix(c.File, "hostile_"+kind+"_") {
			continue
		}
		v := valid[c.Type]
		if v == "" {
			continue
		}
		blobs[v], blobs[c.File] = blobs[c.File], blobs[v]
		swapped = append(swapped, v+"<->"+c.File)
	}
	if len(swapped) == 0 {
		return "", nil, fmt.Errorf("SCHEMA_GATE_PLANT=%s: the corpus has no hostile_%s_ vector beside a valid one: nothing was planted", plant, kind)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	manifest, err := os.ReadFile(filepath.Join(corpusDir, "manifest.tsv"))
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.tsv"), manifest, 0o644); err != nil {
		return "", nil, err
	}
	for _, c := range cases {
		if err := os.WriteFile(filepath.Join(dir, c.File), blobs[c.File], 0o644); err != nil {
			return "", nil, err
		}
	}
	return dir, swapped, nil
}
