package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// THE PLANTED ORACLE, for the negative controls of every leg
// (make/widen-18-gate-controls.mk verify-tablegen-negative-control-<lang>;
// 2026-11-24). The emitter sabotage proves the gate red for JavaScript only;
// here the ORACLE is made wrong instead, the same way for every leg. Under
// SCHEMA_GATE_PLANT=tablegen every case's first expected answer (Want1) has
// its last byte inverted in a copy of the pack the legs read, exactly as
// --only writes its narrowed pack, so a leg whose gate really compares its
// answers goes red by name, and one that compared its output with itself
// would stay green.
func plantTablegen(cases []CorpusCase, dir string) (string, []CorpusCase, int, error) {
	planted := make([]CorpusCase, len(cases))
	n := 0
	for i, c := range cases {
		if len(c.Want1) > 0 {
			c.Want1 = append([]byte(nil), c.Want1...)
			c.Want1[len(c.Want1)-1] ^= 0xff
			n++
		}
		planted[i] = c
	}
	if n == 0 {
		return "", nil, 0, fmt.Errorf("SCHEMA_GATE_PLANT=tablegen: no case carries an expected answer: nothing was planted")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, 0, err
	}
	if err := os.WriteFile(filepath.Join(dir, "corpus.pack"), PackCases(planted), 0o644); err != nil {
		return "", nil, 0, err
	}
	return dir, planted, n, nil
}
