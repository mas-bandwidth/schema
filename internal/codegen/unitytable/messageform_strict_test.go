package unitytable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityMessageFormStrict holds the generated C# (unity) announcement read
// to STRICT MODE (docs/SPEC-TABLES.md §3.3): "STRICT MODE IS OPT-IN, and the
// default is unchanged ... an announcement whose build version is not the
// unit's own is refused by the name `build_version_mismatch`, sets no
// vocabulary, keeps the peer's build version, moves no counter and is never
// `wire_damaged`, and is terminal like every refused first announcement", in
// THE LANGUAGE-NEUTRAL SHAPE: one boolean on the vocabulary, false when it is
// made, and a refusal carrying the peer's build version (remote) and the
// reader's own (own). The endpoint is the unit's own announcement with its
// build-version word replaced, so the reader sees its own table announced at
// another build version.
func TestUnityMessageFormStrict(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect strict announcement")
	const schema = `package probe

fixed table Root {
	a int32
	b bool
	c uint8
}
`
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityStrictMain, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ROW strict_mode: ok") || code != 0 {
		t.Fatalf("strict mode (exit %d):\n%s", code, out)
	}
}

const unityStrictMain = `using System;
using Probe;

static class ProbeMain
{
    static byte[] Endpoint(ulong version)
    {
        byte[] ann = Schema.Announce().ToArray();
        for (int i = 0; i < 8; i++) { ann[3 + i] = (byte)(version >> (8 * i)); }
        return ann;
    }
    static int Fail(string what) { Console.WriteLine("ROW strict_mode: FAILED " + what); return 1; }
    static int Main()
    {
        ulong own = Schema.BuildVersion, remote = own ^ ulong.MaxValue;
        var tolerant = new Schema.TableVocabulary();
        if (tolerant.Strict) { return Fail("a vocabulary is not made tolerant"); }
        var report = new Schema.TableReport();
        if (!Schema.AnnounceRead(tolerant, Endpoint(remote), ref report) || report.Refused || tolerant.BuildVersion != remote) { return Fail("tolerant: " + report.ReasonText); }
        var same = new Schema.TableVocabulary { Strict = true };
        report = new Schema.TableReport();
        if (!Schema.AnnounceRead(same, Endpoint(own), ref report) || report.Refused || report.BuildVersionRemote != 0 || report.BuildVersionOwn != 0) { return Fail("strict same: " + report.ReasonText); }
        var strict = new Schema.TableVocabulary { Strict = true };
        report = new Schema.TableReport();
        if (Schema.AnnounceRead(strict, Endpoint(remote), ref report) || !report.Refused || report.Damaged
            || report.Reason != Schema.TableReport.BuildVersionMismatch || report.ReasonText != "build_version_mismatch") { return Fail("strict other: " + report.ReasonText); }
        if (report.BuildVersionRemote != remote || report.BuildVersionOwn != own) { return Fail("the refusal does not carry both versions"); }
        if (strict.Announced || strict.Count != 0 || strict.BuildVersion != remote) { return Fail("the refusal set a vocabulary or lost the peer's build version"); }
        if (report.Unknown != 0 || report.Widened != 0 || report.Retained != 0 || report.RetainLost != 0) { return Fail("the refusal moved a counter"); }
        var second = new Schema.TableReport();
        if (Schema.AnnounceRead(strict, Endpoint(own), ref second) || second.ReasonText != "second_announcement" || strict.Announced) { return Fail("after a strict refusal: " + second.ReasonText); }
        Console.WriteLine("ROW strict_mode: ok");
        return 0;
    }
}
`
