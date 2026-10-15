package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

// THE AMPLIFICATION ROW, ONE CEILING PER FORM (schema#466, docs/SPEC-TABLES.md
// §6.5). Certify's seed 2 found graphdemo.Scene as a MESSAGE commanding 576
// region bytes from 7 wire bytes, the framing's exact answer, against the
// table wire's ceiling of 512. The message form is held to its own ceiling,
// derived from its bitpacked framing (§3.3), and the table wire keeps its rule.
func amplifyRoot(t *testing.T, message bool) *wireRoot {
	t.Helper()
	_, _, units := corpus(t)
	root, err := newWireRoot(units, "graphdemo", "Scene", message, false)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func agreeingReply(ans oracleAnswer) legReply {
	return legReply{loaded: true, report: ans.report, measure: ans.bytes, saved: ans.encoded, saveFail: ans.encFail}
}

func TestMessageSeed2AmplificationIsInsideTheMessageCeiling(t *testing.T) {
	root := amplifyRoot(t, true)
	data, err := hex.DecodeString("0225a709001004")
	if err != nil {
		t.Fatal(err)
	}
	ans, err := root.oracle(data)
	if err != nil {
		t.Fatal(err)
	}
	if !ans.exact || ans.bytes != 576 {
		t.Fatalf("the oracle sizes the seed-2 batch at %d (exact %v), want exactly 576", ans.bytes, ans.exact)
	}
	if got := root.messageAmplificationCeiling(len(data)); got != 1320 {
		t.Fatalf("message ceiling at 7 wire bytes = %d, want 1320 (6 x (176+16) + 3 x (40+16))", got)
	}
	if v := wireVerdict(root, agreeingReply(ans), ans, len(data)); v != "" {
		t.Fatalf("the seed-2 message is a divergence: %s", v)
	}
}

func TestMessageAboveItsCeilingIsRefused(t *testing.T) {
	root := amplifyRoot(t, true)
	data, err := hex.DecodeString("0225a709001004")
	if err != nil {
		t.Fatal(err)
	}
	ans, err := root.oracle(data)
	if err != nil {
		t.Fatal(err)
	}
	// a leg that commands one byte past the message ceiling, against an
	// oracle bound that would let it through, is still refused by the row
	ceiling := root.messageAmplificationCeiling(len(data))
	ans.exact, ans.bytes = false, ceiling+1000
	reply := agreeingReply(ans)
	reply.measure = ceiling + 1
	v := wireVerdict(root, reply, ans, len(data))
	if !strings.Contains(v, "amplification ceiling is 1320") {
		t.Fatalf("a message measure above its ceiling passed the row: %q", v)
	}
}

func TestTableWireKeepsItsTwoByteCeiling(t *testing.T) {
	root := amplifyRoot(t, false)
	if got := root.amplificationCeiling(7); got != 512 {
		t.Fatalf("table-wire ceiling at 7 wire bytes = %d, want 512", got)
	}
	if got := root.formCeiling(7); got != 512 {
		t.Fatalf("a table-wire root is held to %d, want its own 512", got)
	}
	ans := oracleAnswer{bytes: 10000}
	reply := legReply{loaded: true, measure: 576}
	if v := wireVerdict(root, reply, ans, 7); !strings.Contains(v, "amplification ceiling is 512") {
		t.Fatalf("576 region bytes from 7 table-wire bytes passed the row: %q", v)
	}
}
