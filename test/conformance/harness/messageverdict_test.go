package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageVerdictCorpus holds the committed message-fuzz frames the message
// scorer is held to (review F-114), replayable by any leg with
// `conformance-harness message-fuzz --replay <frame> --unit tblm1 --root Msg`:
//
//	historical-kind.frame  a peer's save of tblm1.Msg{seq: 40} announcing seq
//	                       as int32 where this build declares uint32: the
//	                       reference refuses it (kind mismatch)
//	unchanged.frame        the same value under the true vocabulary: the
//	                       reference accepts it (the control)
const messageVerdictCorpus = "testdata/conformance/tables/message-verdict"

// buildMessageVerdictFrames rebuilds the two committed frames from the
// committed tblm1 schema.
func buildMessageVerdictFrames(t *testing.T, r *messageFuzzRoot, u *units) (historical, unchanged []byte) {
	t.Helper()
	irUnit, err := u.get("tblm1")
	if err != nil {
		t.Fatal(err)
	}
	inst := r.model.New(r.def)
	set := false
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == "seq" {
			inst.Fields[i].Cell.U = 40
			set = true
		}
	}
	if !set {
		t.Fatal("tblm1.Msg has no seq field")
	}
	peer := ir.BuildVersion(irUnit) ^ 0x5eed
	frame := func(entries []ir.TableVocabularyEntry) []byte {
		batch, err := tablewire.EncodeMessagesWithVocabulary(r.model, []*tabletext.Instance{inst}, entries)
		if err != nil {
			t.Fatal(err)
		}
		return packMessageFuzzFrame(wrapVocabularyAnnouncement(peer, ir.TableVocabularyBytes(entries)), batch)
	}
	old := append([]ir.TableVocabularyEntry(nil), r.entries...)
	changed := 0
	for i := range old {
		if old[i].Kind == uint8(ir.TableKindU32) {
			old[i].Kind = uint8(ir.TableKindI32)
			changed++
		}
	}
	if changed != 1 {
		t.Fatalf("tblm1.Msg's vocabulary has %d uint32 entries, want exactly seq", changed)
	}
	return frame(old), frame(r.entries)
}

// The message scorer holds a leg's refusal to the file form's rule: where the
// reference refuses a batch, the leg's report must NAME a refusal (refused or
// malformed, for any reason). A leg answering no value and no refusal on a
// real historical save discarded it silently, and diverges (review F-114);
// the same silent leg on the unchanged control diverges too.
func TestMessageVerdictNeedsANamedRefusal(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	u := newUnits(m)
	roots, rootMap, err := buildMessageFuzzRoots(u, messageFuzzOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ri, ok := rootMap["tblm1.Msg"]
	if !ok {
		t.Fatal("message-fuzz has no tblm1.Msg root")
	}
	r := roots[ri]
	historical, unchanged := buildMessageVerdictFrames(t, r, u)
	if os.Getenv("SCHEMA_UPDATE_MESSAGE_VERDICT") != "" {
		if err := os.MkdirAll(messageVerdictCorpus, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string][]byte{"historical-kind.frame": historical, "unchanged.frame": unchanged} {
			if err := os.WriteFile(filepath.Join(messageVerdictCorpus, name), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, body := range map[string][]byte{"historical-kind.frame": historical, "unchanged.frame": unchanged} {
		committed, err := os.ReadFile(filepath.Join(messageVerdictCorpus, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(committed, body) {
			t.Fatalf("%s/%s is not the frame the tblm1 schema builds (SCHEMA_UPDATE_MESSAGE_VERDICT=1 rewrites it)", messageVerdictCorpus, name)
		}
	}

	oracle := func(frame []byte) oracleAnswer {
		ans, err := r.oracle(frame)
		if err != nil {
			t.Fatal(err)
		}
		return ans
	}
	hist, ctrl := oracle(historical), oracle(unchanged)
	if hist.accepted || !ctrl.accepted {
		t.Fatalf("the reference accepted the historical save %v and the control %v, want false and true", hist.accepted, ctrl.accepted)
	}

	silent := legReply{}
	for _, tc := range []struct {
		name string
		rep  legReply
		ans  oracleAnswer
		ok   bool
	}{
		{"historical, silent leg", silent, hist, false},
		{"historical, generic refusal", legReply{report: Counts{Refused: true}}, hist, true},
		{"historical, malformed", legReply{report: Counts{Malformed: true}}, hist, true},
		{"historical, a value", legReply{loaded: true, saved: ctrl.encoded}, hist, false},
		{"unchanged, silent leg", silent, ctrl, false},
		{"unchanged, refused", legReply{report: Counts{Refused: true}}, ctrl, false},
		{"unchanged, the value", legReply{loaded: true, saved: ctrl.encoded}, ctrl, true},
	} {
		if why := messageVerdict(tc.rep, tc.ans); (why == "") != tc.ok {
			t.Errorf("%s: messageVerdict = %q, want ok=%v", tc.name, why, tc.ok)
		}
	}
}
