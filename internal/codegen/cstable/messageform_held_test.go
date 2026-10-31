package cstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
)

// TestMessageFormHeldCSharp asserts, one subtest per clause, the refusals and
// the wire shape the message form names on the generated C# surface
// (docs/SPEC-TABLES.md §3.3). One `fixed table` root is generated, compiled and
// run once against the .NET SDK; each Go subtest reads its own clause's verdict
// out of that run. A clause that printed nothing DID NOT RUN — the assembly did
// not build or the name moved — and that is a failure and never a pass.
//
// The C# table wire carries no serialize dependency (the generated home's own
// header), so the project compiles the generated unit standing alone; the
// sibling `serialize.cs` runtime, which the PACKET emitter's tests need, is not
// a requirement here.
func TestMessageFormHeldCSharp(t *testing.T) {
	t.Parallel()
	out, err := csHeldRun(t)
	clauses := []struct {
		name string
		page string
	}{
		{"no_vocabulary", "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME."},
		{"second_announcement", "NO RE-ANNOUNCEMENT, EVER. ... REFUSAL IS TERMINAL, and a refused FIRST announcement is the one the connection gets."},
		{"vocabulary_too_large", "`vocabulary_too_large` covers both bounds, the entry count and the vocabulary's bytes."},
		{"announcement_strict_checks", "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS ... A FAILED STRICT CHECK IS MALFORMED, not a refusal."},
		{"batch_too_large", "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... `M` ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME."},
		{"damage_inside_body", "damage anywhere in a form-`2` batch refuses the whole batch, `malformed` counts, and nothing the read produced is a value to use."},
		{"bytes_after_pad", "BYTES AFTER THE PAD ARE MALFORMED ... damage anywhere in a form-`2` batch refuses the whole batch."},
		{"form_byte", "a form a reader does not know is a REFUSAL by name and never damage ... a reader handed a form-`2` wire where a file was expected refuses by name."},
		{"skip_depth_body_below_128", "The skip walk is bounded at depth 128."},
		{"skip_depth_body_at_128", "The skip walk is bounded at depth 128."},
		{"skip_depth_above_128_body", "The skip walk is bounded at depth 128: a nested body past 128 levels is refused as malformed."},
		{"skip_depth_union_below_128", "The skip walk is bounded at depth 128."},
		{"skip_depth_union_at_128", "The skip walk is bounded at depth 128."},
		{"skip_depth_above_128_union", "The skip walk is bounded at depth 128: a union arm past 128 levels is refused as malformed."},
		{"skip_depth_array_below_128", "The skip walk is bounded at depth 128."},
		{"skip_depth_array_at_128", "The skip walk is bounded at depth 128."},
		{"skip_depth_above_128_array", "The skip walk is bounded at depth 128: an element skip past 128 levels is refused as malformed."},
		{"skip_depth_keyed_fixed_at_128", "The skip walk is bounded at depth 128: a fixed-width keyed array at depth 128 does not charge element depth."},
		{"skip_depth_keyed_nopayload_at_128", "The skip walk is bounded at depth 128: a no-payload keyed array at depth 128 does not charge element depth."},
		{"skip_depth_above_128_keyed", "The skip walk is bounded at depth 128: a keyed nested-body element past 128 levels is refused as malformed."},
		{"skip_depth_continuation", "The skip walk is bounded at depth 128."},
		{"skip_depth_truncation", "The skip walk is bounded at depth 128."},
	}
	for _, clause := range clauses {
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			csHeldAssert(t, out, err, "clause/"+clause.name)
		})
	}
	for _, conn := range csHeldConns {
		t.Run("connection_"+conn.key, func(t *testing.T) {
			t.Parallel()
			csHeldAssert(t, out, err, "row/"+conn.key)
		})
	}
	for _, conn := range csHeldRuntimeConns {
		t.Run("connection_"+conn.key, func(t *testing.T) {
			t.Parallel()
			rowOut, rowErr := csHeldRuntimeAttempt(t, conn)
			if rowErr == nil {
				t.Fatalf("connection_%s built without the sibling runtime; its announcement check must run", conn.key)
			}
			// The row is named as not run only when the build's own
			// diagnostics say the sibling `../serialize.cs` runtime is what is
			// missing; any other compiler error is a generated-code or
			// compiler regression this leg must show, never hide (§3.3).
			if !csHeldMissingRuntime(rowOut) {
				t.Fatalf("connection_%s failed to build for a reason other than the sibling ../serialize.cs runtime:\n%s", conn.key, rowOut)
			}
			// An OWED GAP, not a toolchain absence: this attempt compiles the
			// unit without the sibling runtime on purpose and no leg here yet
			// links ../serialize.cs to run the announcement check, so the row
			// stays a named skip even under SCHEMA_REQUIRE_CORPUS (staging the
			// sibling does not change it). make tables-cs-fixed-rows lists it.
			t.Skipf("not run: the packet emitter names the sibling ../serialize.cs runtime this checkout does not carry:\n%s", csHeldFirstErrors(rowOut))
		})
	}
	for _, msg := range csHeldMsgs {
		t.Run("message_"+msg.name, func(t *testing.T) {
			t.Parallel()
			csHeldAssert(t, out, err, "row/"+msg.name)
		})
	}
}

const csHeldSchema = `package probe

fixed table Root {
	a int32
	b bool
	c uint8
}
`

var (
	csHeldOnce sync.Once
	csHeldOut  []byte
	csHeldErr  error
)

func csHeldRun(t *testing.T) ([]byte, error) {
	t.Helper()
	csHeldOnce.Do(func() { csHeldOut, csHeldErr = csHeldBuild(t) })
	return csHeldOut, csHeldErr
}

func csHeldBuild(t *testing.T) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	gen := filepath.Join(dir, "gen")
	if err := os.MkdirAll(gen, 0o700); err != nil {
		t.Fatal(err)
	}
	var sources []string
	schemas := append([]string{csHeldSchema}, csHeldUnitSchemas(t)...)
	for i, schema := range schemas {
		u := unitFrom(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		// The manifest units' enums ride in the PACKET emitter's output, which
		// the table file names; the clause probe declares none.
		if i > 0 {
			packet, err := csharp.Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			maps.Copy(files, packet)
		}
		unit := filepath.Join(gen, fmt.Sprintf("u%02d", i))
		if err := os.MkdirAll(unit, 0o700); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			path := filepath.Join(unit, name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, path)
		}
	}
	held := filepath.Join(dir, "Held.cs")
	if err := os.WriteFile(held, []byte(csHeldCompose(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	// The Roslyn compiler is invoked through the host directly, which is the
	// SDK's own compiler without the CLI's first-run configuration or NuGet's
	// restore — neither is needed for a project that references no package.
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "csmsgheld.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, held)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return compileOut, fmt.Errorf("compile: %w", compileErr)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "csmsgheld.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	runOut, runErr := run.CombinedOutput()
	return append(compileOut, runOut...), runErr
}

// csHeldRuntimeAttempt generates one csHeldRuntimeUnits unit with both emitters
// and compiles it as a library, returning the compiler's own output and error.
// The unit is compiled whole, with its packet file kept, so the refusal the
// missing sibling `../serialize.cs` runtime produces is the build's real one,
// not a guess: the connection row is named as not run only when this attempt
// says the runtime it needs is what is missing.
func csHeldRuntimeAttempt(t *testing.T, conn csHeldConn) (string, error) {
	t.Helper()
	var unit csHeldUnit
	for _, u := range csHeldRuntimeUnits {
		if u.pkg == conn.pkg {
			unit = u
		}
	}
	if unit.pkg == "" {
		t.Fatalf("no runtime unit for %s", conn.pkg)
	}
	dir := t.TempDir()
	u := unitFrom(t, csHeldSchemaFor(t, unit))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	csc, refs, _ := csDotnetToolchain(t)
	dotnet := csDotnet()
	lib := filepath.Join(dir, "csmsgrow.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:library", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + lib}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	out, err := compile.CombinedOutput()
	return string(out), err
}

// csHeldRuntimeTypes are the names the sibling `../serialize.cs` runtime
// provides and the PACKET emitter's generated unit references: the `Serialize`
// namespace and its stream and batch ref structs. A build that fails only
// because this checkout does not stage that sibling reports each of these as an
// unresolved name (error CS0246, docs/SPEC-TABLES.md §3.3's wire functions call
// the packet runtime); any other diagnostic is a generated-code or compiler
// regression.
var csHeldRuntimeTypes = []string{"Serialize", "ReadStream", "WriteStream", "ReadBatch", "WriteBatch"}

// csHeldMissingRuntime reports whether an attempt's compiler diagnostics
// identify the sibling `../serialize.cs` runtime as what is missing: at least
// one error, and every error a CS0246 whose unresolved name is one of
// csHeldRuntimeTypes. A diagnosis with any other error, or no error at all, is
// not the missing runtime and must fail the row instead of being named not run
// (docs/SPEC-TABLES.md §3.3).
func csHeldMissingRuntime(out string) bool {
	sawError := false
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.Contains(line, "error CS") {
			continue
		}
		sawError = true
		if !strings.Contains(line, "error CS0246:") {
			return false
		}
		found := false
		for _, name := range csHeldRuntimeTypes {
			if strings.Contains(line, "'"+name+"'") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return sawError
}

// csHeldFirstErrors keeps the first compiler error lines of an attempt's
// output, so a not-run skip names what the build actually refused on instead
// of an assumed reason.
func csHeldFirstErrors(out string) string {
	var lines []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "error CS") {
			lines = append(lines, strings.TrimSpace(line))
			if len(lines) == 3 {
				break
			}
		}
	}
	if len(lines) == 0 {
		return out
	}
	return strings.Join(lines, "\n")
}

// csDotnet is the dotnet command the leg's Makefile pins, DOTNET or the name on
// the path.
func csDotnet() string {
	if dotnet := os.Getenv("DOTNET"); dotnet != "" {
		return dotnet
	}
	return "dotnet"
}

// csDotnetToolchain locates the SDK's own Roslyn compiler, its reference
// assemblies and its runtime version, all as siblings of the resolved dotnet
// host, so no CLI command is run and no package is restored.
func csDotnetToolchain(t *testing.T) (csc string, refs []string, runtime string) {
	t.Helper()
	path, err := exec.LookPath(csDotnet())
	if err != nil {
		t.Fatalf("dotnet: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	root := filepath.Dir(resolved)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	sort.Strings(cscs)
	csc = cscs[len(cscs)-1]
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net*"))
	if len(refDirs) == 0 {
		t.Fatalf("no reference assemblies under %s", root)
	}
	sort.Strings(refDirs)
	refs, _ = filepath.Glob(filepath.Join(refDirs[len(refDirs)-1], "*.dll"))
	if len(refs) == 0 {
		t.Fatalf("no reference assemblies in %s", refDirs[len(refDirs)-1])
	}
	runtimes, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(runtimes) == 0 {
		t.Fatalf("no shared runtime under %s", root)
	}
	sort.Strings(runtimes)
	runtime = filepath.Base(runtimes[len(runtimes)-1])
	return csc, refs, runtime
}

// csDotnetEnv is the environment a dotnet child runs under: HOME and the temp
// directory point inside the test's own tree, so the host writes only there.
func csDotnetEnv(home, tmp string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "HOME="),
			strings.HasPrefix(e, "TMPDIR="),
			strings.HasPrefix(e, "TMP="),
			strings.HasPrefix(e, "TEMP="),
			strings.HasPrefix(e, "DOTNET_"):
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"DOTNET_NOLOGO=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"HOME="+home,
		"DOTNET_CLI_HOME="+home,
		"TMPDIR="+tmp,
		"TMP="+tmp,
		"TEMP="+tmp,
	)
}

func csHeldAssert(t *testing.T, out []byte, runErr error, row string) {
	t.Helper()
	text := string(out)
	if strings.Contains(text, "ROW "+row+": ok") {
		return
	}
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, "ROW "+row) || strings.HasPrefix(strings.TrimSpace(line), "error") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s DID NOT RUN: the assembly did not build, or the name moved\n%s", row, text)
	}
	t.Fatalf("%s:\n%s", row, strings.Join(lines, "\n"))
}

const csHeldMain = `// generated by internal/codegen/cstable's message-form harness: one probe per
// clause of docs/SPEC-TABLES.md §3.3, against the generated C# unit in gen/.
using System;
using Probe;

static class Held
{
    static int bad = 0;
    static void Ok(string row) { Console.WriteLine("ROW clause/" + row + ": ok"); }
    static void Fail(string row, string what) { Console.WriteLine("ROW clause/" + row + ": FAILED " + what); bad++; }

    static byte[] LE64(ulong v) { byte[] b = new byte[8]; System.Buffers.Binary.BinaryPrimitives.WriteUInt64LittleEndian(b, v); return b; }
    static byte[] Cat(params byte[][] parts) { var l = new System.Collections.Generic.List<byte>(); foreach (var p in parts) l.AddRange(p); return l.ToArray(); }
    static byte[] Leb(ulong v) { var o = new System.Collections.Generic.List<byte>(); for (;;) { byte c = (byte)(v & 0x7f); v >>= 7; if (v != 0) c |= 0x80; o.Add(c); if (v == 0) return o.ToArray(); } }
    static byte[] VersionField(ulong v) { return Cat(new byte[]{1,9}, LE64(v)); }
    static byte[] VocabSub(byte elem, byte[] vocab) { return Cat(new byte[]{elem}, Leb((ulong)vocab.Length), vocab); }
    static byte[] VocabField(byte elem, byte[] vocab) { byte[] sub = VocabSub(elem, vocab); return Cat(new byte[]{2,14}, Leb((ulong)sub.Length), sub); }
    static byte[] Announcement(byte[] fields) { return Cat(new byte[]{1}, fields, new byte[]{0}, LE64(0xfffffffffffffffe), LE64(0xfffffffffffffffd), LE64(2)); }

    static TableVocabulary Open()
    {
        var v = new TableVocabulary();
        byte[] ann = Schema.Announce().ToArray();
        var report = new TableReport();
        Schema.AnnounceRead(v, ann, report);
        return v;
    }

    static Root[] Bodies(int n)
    {
        var a = new Root[n];
        for (int i = 0; i < n; ++i) { a[i] = new Root(); }
        return a;
    }

    public static int Run()
    {
        NoVocabulary();
        SecondAnnouncement();
        VocabularyTooLarge();
        StrictChecks();
        BatchTooLarge();
        DamageInsideBody();
        BytesAfterPad();
        FormByte();
        SkipDepth();
        Console.WriteLine(bad == 0 ? "cs message form: every clause green" : "cs message form: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }

    // "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
    // decoded, no counter moves and malformed does not fire."
    static void NoVocabulary()
    {
        const string row = "no_vocabulary";
        Root[] values = Bodies(1);
        long m = Schema.RootMeasureMessages(values);
        byte[] buf = new byte[m];
        var save = new TableReport();
        if (Schema.RootSaveMessages(values, buf, save) != m) { Fail(row, "save"); return; }
        var vocab = new TableVocabulary();
        Root[] storage = Bodies(1);
        var report = new TableReport();
        Schema.RootLoadMessages(storage, buf, vocab, report, out int count);
        if (count != 0 || !report.Refused || report.Reason != "no_vocabulary" || report.Malformed) { Fail(row, "count=" + count + " refused=" + report.Refused + " reason=" + report.Reason + " malformed=" + report.Malformed); return; }
        Ok(row);
    }

    // "NO RE-ANNOUNCEMENT, EVER. ... REFUSAL IS TERMINAL, and a refused FIRST
    // announcement is the one the connection gets ... every announcement after it
    // is refused as second_announcement ... every body after it is refused for
    // want of one."
    static void SecondAnnouncement()
    {
        const string row = "second_announcement";
        byte[] ann = Schema.Announce().ToArray();
        var v = new TableVocabulary();
        var first = new TableReport();
        if (Schema.AnnounceRead(v, ann, first) != Schema.TableWire.Verdict.Ok || !v.Announced) { Fail(row, "first announcement"); return; }
        int before = v.Count;
        var second = new TableReport();
        Schema.AnnounceRead(v, ann, second);
        if (!second.Refused || second.Reason != "second_announcement" || second.Malformed) { Fail(row, "second refused=" + second.Refused + " reason=" + second.Reason); return; }
        if (v.Count != before) { Fail(row, "a second announcement replaced the vocabulary"); return; }
        var w = new TableVocabulary();
        var rejected = new TableReport();
        Schema.AnnounceRead(w, new byte[]{9,9,9}, rejected);
        if (!rejected.Refused || rejected.Reason != "newer_form" || w.Announced) { Fail(row, "rejected first: reason=" + rejected.Reason + " announced=" + w.Announced); return; }
        var later = new TableReport();
        Schema.AnnounceRead(w, ann, later);
        if (!later.Refused || later.Reason != "second_announcement" || w.Announced) { Fail(row, "later: reason=" + later.Reason); return; }
        Root[] values = Bodies(1);
        long m = Schema.RootMeasureMessages(values);
        byte[] buf = new byte[m];
        var save = new TableReport();
        Schema.RootSaveMessages(values, buf, save);
        Root[] storage = Bodies(1);
        var body = new TableReport();
        Schema.RootLoadMessages(storage, buf, w, body, out int count);
        if (count != 0 || body.Reason != "no_vocabulary") { Fail(row, "body on the refused connection: count=" + count + " reason=" + body.Reason); return; }
        Ok(row);
    }

    // "vocabulary_too_large covers both bounds, the entry count and the
    // vocabulary's bytes."
    static void VocabularyTooLarge()
    {
        const string row = "vocabulary_too_large";
        byte[] ann = Schema.Announce().ToArray();
        var entries = new TableVocabulary { MaxEntries = 1 };
        var report = new TableReport();
        Schema.AnnounceRead(entries, ann, report);
        if (!report.Refused || report.Reason != "vocabulary_too_large" || report.Malformed || entries.Announced) { Fail(row, "entry count: reason=" + report.Reason); return; }
        var bytes = new TableVocabulary { MaxBytes = 4 };
        var byteReport = new TableReport();
        Schema.AnnounceRead(bytes, ann, byteReport);
        if (!byteReport.Refused || byteReport.Reason != "vocabulary_too_large" || byteReport.Malformed || bytes.Announced) { Fail(row, "vocabulary bytes: reason=" + byteReport.Reason); return; }
        Ok(row);
    }

    // "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS: the
    // build version present, exactly once, kind 9, eight bytes wide, and the
    // vocabulary present, exactly once, kind 14, element kind 6 ... A FAILED
    // STRICT CHECK IS MALFORMED, not a refusal ... The build version is kept the
    // moment it is read, refusal or not."
    static void StrictChecks()
    {
        const string row = "announcement_strict_checks";
        ulong version = 0x1122334455667788;
        byte[][] cases = new byte[][] {
            Announcement(VocabField(6, Array.Empty<byte>())),
            Announcement(Cat(VersionField(version), VersionField(version), VocabField(6, Array.Empty<byte>()))),
            Announcement(Cat(new byte[]{1,5,0,0,0,0,0,0,0,0}, VocabField(6, Array.Empty<byte>()))),
            Announcement(new byte[]{1,9,0,0,0,0}),
            Announcement(VersionField(version)),
            Announcement(Cat(VersionField(version), VocabField(13, Array.Empty<byte>()))),
            Announcement(Cat(VersionField(version), VocabField(9, Array.Empty<byte>()))),
        };
        string[] names = {"version missing","version twice","version not kind 9","version fewer than eight bytes","vocabulary missing","vocabulary not kind 14","vocabulary element not kind 6"};
        for (int i = 0; i < cases.Length; ++i)
        {
            var v = new TableVocabulary();
            var report = new TableReport();
            Schema.AnnounceRead(v, cases[i], report);
            if (!report.Malformed || report.Refused || v.Announced) { Fail(row, names[i] + ": malformed=" + report.Malformed + " refused=" + report.Refused + " announced=" + v.Announced); return; }
        }
        var keep = new TableVocabulary();
        var keepReport = new TableReport();
        Schema.AnnounceRead(keep, Announcement(Cat(VersionField(version), new byte[]{2,13})), keepReport);
        if (!keepReport.Malformed || keep.BuildVersion != version || keep.Announced) { Fail(row, "the refused announcement did not keep the build version: " + keep.BuildVersion.ToString("x16")); return; }
        Ok(row);
    }

    // "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... MeasureMessages
    // and SaveMessages both refuse and return -1" and "M ABOVE THE CALLER'S
    // CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... The reader sets the
    // returned count to the wire's M before it returns the refusal."
    static void BatchTooLarge()
    {
        const string row = "batch_too_large";
        Root[] huge = Bodies(257);
        if (Schema.RootMeasureMessages(huge) != -1) { Fail(row, "measure over 256 bodies did not refuse"); return; }
        var save = new TableReport();
        if (Schema.RootSaveMessages(huge, new byte[8], save) != -1 || !save.Refused || save.Reason != "batch_too_large") { Fail(row, "save over 256 bodies: reason=" + save.Reason); return; }
        var vocab = Open();
        if (!vocab.Announced) { Fail(row, "announce"); return; }
        Root[] values = Bodies(2);
        long m = Schema.RootMeasureMessages(values);
        byte[] buf = new byte[m];
        var wrote = new TableReport();
        if (Schema.RootSaveMessages(values, buf, wrote) != m) { Fail(row, "save two bodies"); return; }
        Root[] storage = Bodies(1);
        var report = new TableReport();
        Schema.RootLoadMessages(storage, buf, vocab, report, out int count);
        if (!report.Refused || report.Reason != "batch_too_large" || report.Malformed || count != 2) { Fail(row, "read over capacity: count=" + count + " reason=" + report.Reason); return; }
        Ok(row);
    }

    // "damage anywhere in a form-2 batch refuses the whole batch, malformed
    // counts, and nothing the read produced is a value to use." M is three:
    // body 1 empty, body 2's first reference 0xF above the announced entry
    // count, body 3 empty. A reference is four bits, low bit first, so byte 0
    // holds body 1's low nibble and body 2's high nibble.
    static void DamageInsideBody()
    {
        const string row = "damage_inside_body";
        var vocab = Open();
        if (!vocab.Announced) { Fail(row, "announce"); return; }
        byte[] wire = new byte[]{2, 2, 0xF0, 0x00};
        Root[] storage = Bodies(3);
        var report = new TableReport();
        Schema.RootLoadMessages(storage, wire, vocab, report, out int count);
        if (count != 0 || !report.Malformed || report.Refused) { Fail(row, "count=" + count + " malformed=" + report.Malformed); return; }
        Ok(row);
    }

    // "BYTES AFTER THE PAD ARE MALFORMED ... the batch ends at the pad to the
    // byte boundary, and a buffer with bytes left over describes no batch this
    // reader can name."
    static void BytesAfterPad()
    {
        const string row = "bytes_after_pad";
        var vocab = Open();
        if (!vocab.Announced) { Fail(row, "announce"); return; }
        Root[] values = Bodies(1);
        long m = Schema.RootMeasureMessages(values);
        byte[] buf = new byte[m];
        var wrote = new TableReport();
        if (Schema.RootSaveMessages(values, buf, wrote) != m) { Fail(row, "save"); return; }
        byte[] padded = new byte[buf.Length + 1];
        Array.Copy(buf, padded, buf.Length);
        Root[] storage = Bodies(1);
        var report = new TableReport();
        Schema.RootLoadMessages(storage, padded, vocab, report, out int count);
        if (count != 0 || !report.Malformed) { Fail(row, "count=" + count + " malformed=" + report.Malformed); return; }
        Ok(row);
    }

    // "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does
    // not know is a REFUSAL by name and never damage" and "a reader handed a
    // form-2 wire where a file was expected refuses by name."
    static void FormByte()
    {
        const string row = "form_byte";
        var vocab = new TableVocabulary();
        foreach (byte form in new byte[]{1, 6})
        {
            Root[] storage = Bodies(1);
            var report = new TableReport();
            Schema.RootLoadMessages(storage, new byte[]{form}, vocab, report, out int count);
            if (count != 0 || !report.Refused || report.Reason != "newer_form") { Fail(row, "form " + form + ": count=" + count + " reason=" + report.Reason); return; }
        }
        var file = new TableVocabulary();
        var fileReport = new TableReport();
        Schema.AnnounceRead(file, new byte[]{2, 0, 0, 0}, fileReport);
        if (!fileReport.Refused || fileReport.Reason != "message_form_as_file") { Fail(row, "form-2 batch as a file: reason=" + fileReport.Reason); return; }
        Ok(row);
    }

    // message_skip_depth (docs/SPEC-TABLES.md, value 128). Nested bodies, union
    // arms and walked array elements are accepted at 127 and at 128. Each
    // above-limit row is a complete zero-padded batch whose only fault is depth
    // past 128, so removing the guard fails that row and leaves the others ok.
    // Truncation drops the root terminator of the at-limit body. 16 + 256 bits
    // is a whole number of bytes, so a pad bit cannot stand in for it.
    static byte[] Pack(int width, int nlead, int lead, int nmid, int mid, int ntail)
    {
        int at = 16;
        int n = nlead + nmid + ntail;
        int bits = 16 + n * width;
        byte[] buf = new byte[(bits + 7) / 8];
        buf[0] = 2;
        buf[1] = 0;
        for (int i = 0; i < n; i++)
        {
            int slot = i < nlead ? lead : (i < nlead + nmid ? mid : 0);
            for (int b = 0; b < width; b++)
            {
                if (((slot >> b) & 1) != 0) buf[at >> 3] |= (byte)(1 << (at & 7));
                at++;
            }
        }
        return buf;
    }
    static TableVocabulary Vocab(params TableMessageEntry[] entries)
    {
        return new TableVocabulary { Entries = entries, Announced = true };
    }
    static TableMessageEntry Entry(ulong id, byte kind)
    {
        return new TableMessageEntry { Id = id, Kind = kind, Shape = new TableMessageShape() };
    }
    static void ExpectOk(string row, TableVocabulary vocab, byte[] buf)
    {
        Root[] storage = Bodies(1);
        var report = new TableReport();
        var verdict = Schema.RootLoadMessages(storage, buf, vocab, report, out int count);
        if (verdict != Schema.TableWire.Verdict.Ok || count != 1 || report.Malformed || report.Unknown != 1 || storage[0].A != 0)
        { Fail(row, "verdict=" + verdict + " count=" + count + " malformed=" + report.Malformed + " unknown=" + report.Unknown + " a=" + storage[0].A); return; }
        Ok(row);
    }
    static void ExpectBad(string row, TableVocabulary vocab, byte[] buf)
    {
        Root[] storage = Bodies(1);
        var report = new TableReport();
        var verdict = Schema.RootLoadMessages(storage, buf, vocab, report, out int count);
        if (verdict != Schema.TableWire.Verdict.Damaged || count != 0 || !report.Malformed)
        { Fail(row, "verdict=" + verdict + " count=" + count + " malformed=" + report.Malformed); return; }
        Ok(row);
    }
    static void SkipDepth()
    {
        var body = Vocab(Entry(1, 13));
        var arm = Vocab(Entry(1, 15));
        var array = Vocab(Entry(1, 13), new TableMessageEntry { Id = 2, Kind = 14, Shape = new TableMessageShape { Min = 1, Max = 1, Elem = 13, Inner = new TableMessageShape() } });
        var keyed6 = Vocab(Entry(1, 13), new TableMessageEntry { Id = 2, Kind = 16, Shape = new TableMessageShape { Min = 0, Max = 3, Elem = 6, Inner = new TableMessageShape() } });
        var keyed32 = Vocab(Entry(1, 13), new TableMessageEntry { Id = 2, Kind = 16, Shape = new TableMessageShape { Min = 0, Max = 3, Elem = 32, Inner = new TableMessageShape() } });
        var keyed13 = Vocab(Entry(1, 13), new TableMessageEntry { Id = 2, Kind = 16, Shape = new TableMessageShape { Min = 0, Max = 3, Elem = 13, Inner = new TableMessageShape() } });
        ExpectOk("skip_depth_body_below_128", body, Pack(1, 127, 1, 0, 0, 128));
        byte[] at = Pack(1, 128, 1, 0, 0, 129);
        ExpectOk("skip_depth_body_at_128", body, at);
        ExpectOk("skip_depth_continuation", body, at);
        byte[] cut = Pack(1, 128, 1, 0, 0, 128);
        if (cut.Length * 8 != 16 + 256) { Fail("skip_depth_truncation", "alignment"); }
        else { ExpectBad("skip_depth_truncation", body, cut); }
        ExpectBad("skip_depth_above_128_body", body, Pack(1, 129, 1, 0, 0, 130));
        ExpectOk("skip_depth_union_below_128", arm, Pack(1, 128, 1, 0, 0, 2));
        ExpectOk("skip_depth_union_at_128", arm, Pack(1, 129, 1, 0, 0, 2));
        ExpectBad("skip_depth_above_128_union", arm, Pack(1, 130, 1, 0, 0, 2));
        ExpectOk("skip_depth_array_below_128", array, Pack(2, 125, 1, 1, 2, 127));
        ExpectOk("skip_depth_array_at_128", array, Pack(2, 126, 1, 1, 2, 128));
        ExpectBad("skip_depth_above_128_array", array, Pack(2, 127, 1, 1, 2, 129));
        ExpectOk("skip_depth_keyed_fixed_at_128", keyed6, Pack(2, 128, 1, 2, 2, 139));
        ExpectOk("skip_depth_keyed_nopayload_at_128", keyed32, Pack(2, 128, 1, 2, 2, 131));
        ExpectBad("skip_depth_above_128_keyed", keyed13, Pack(2, 127, 1, 2, 2, 132));
    }
}

static class Program
{
    static int Main() { return Held.Run() + Rows.Run(); }
}
`

// csHeldUnit is one manifest unit this leg generates: its schema directory, the
// package name inside it, the C# namespace that name capitalizes to, and the
// one-letter prefix its aliases take.
type csHeldUnit struct{ dir, pkg, ns, pre string }

var csHeldUnits = []csHeldUnit{
	{"tables/backend", "backenddemo", "Backenddemo", "B"},
	{"tables/vocab", "vocabdemo", "Vocabdemo", "V"},
	{"tables/vocab9", "vocab9demo", "Vocab9demo", "N"},
}

// csHeldRuntimeUnits are the manifest units whose connection rows the Go
// leg answers and this leg's own build does not carry: their packet emitter
// names the sibling `../serialize.cs` runtime this checkout does not stage, and
// this card's rule that such a target is NAMED AS NOT RUN and never faked
// holds. Each row's subtest attempts the build and reports the compiler's own
// refusal, so the not-run status is evidenced rather than assumed.
var csHeldRuntimeUnits = []csHeldUnit{
	{"tables/pointers", "graphdemo", "Graphdemo", "G"},
}

// csHeldConn is one MANIFEST `connection` row: the unit whose whole vocabulary
// it carries, the announcement wire, and the build version the row pins.
type csHeldConn struct{ key, pkg, wire, version string }

var csHeldConns = []csHeldConn{
	{"backend_conn", "backenddemo", "backend_conn.bin", "0x16d0630a084485ae"},
	{"vocab_conn", "vocabdemo", "vocab_conn.bin", "0x40062c876abfc027"},
	{"vocab9_conn", "vocab9demo", "vocab9_conn.bin", "0x3a923c7d0da66dd8"},
}

// csHeldRuntimeConns are the `connection` rows of MANIFEST.txt the main
// build cannot carry (csHeldRuntimeUnits), each with its announcement wire and
// the build version the row pins.
var csHeldRuntimeConns = []csHeldConn{
	{"graph_conn", "graphdemo", "graph_conn.bin", "0xfdaabe4726d81d5c"},
}

// csHeldMsg is one MANIFEST `message` row: one value in BOTH forms, the file
// form (§3) and the message form (§3.3), and the round trip the pair pins.
type csHeldMsg struct{ name, pkg, root, conn, file, msg string }

var csHeldMsgs = []csHeldMsg{
	{"login_full", "backenddemo", "LoginRequest", "backend_conn.bin", "login_full.bin", "login_full_message.bin"},
	{"login_default", "backenddemo", "LoginRequest", "backend_conn.bin", "login_default.bin", "login_default_message.bin"},
	{"match_full", "backenddemo", "MatchResult", "backend_conn.bin", "match_full.bin", "match_full_message.bin"},
	{"match_default", "backenddemo", "MatchResult", "backend_conn.bin", "match_default.bin", "match_default_message.bin"},
	{"store_full", "backenddemo", "StorePurchase", "backend_conn.bin", "store_full.bin", "store_full_message.bin"},
	{"store_default", "backenddemo", "StorePurchase", "backend_conn.bin", "store_default.bin", "store_default_message.bin"},
	{"vocab_wide", "vocabdemo", "Wide09", "vocab_conn.bin", "vocab_wide.bin", "vocab_wide_message.bin"},
	{"vocab_low", "vocabdemo", "Wide00", "vocab_conn.bin", "vocab_low.bin", "vocab_low_message.bin"},
	{"peer_a", "backenddemo", "StorePurchase", "backend_conn.bin", "peer_a.bin", "peer_a_message.bin"},
	{"peer_b", "vocabdemo", "Wide00", "vocab_conn.bin", "peer_b.bin", "peer_b_message.bin"},
	{"vocab9_wide", "vocab9demo", "Wide19", "vocab9_conn.bin", "vocab9_wide.bin", "vocab9_wide_message.bin"},
	{"vocab9_low", "vocab9demo", "Wide00", "vocab9_conn.bin", "vocab9_low.bin", "vocab9_low_message.bin"},
}

// csHeldUnitSchemas reads the manifest units off the tree, each as one source:
// a unit line names either a directory (its *.schema files) or one file, so the
// per-file `package` lines are folded into the one the unit declares
// (parseManifest's rule in the Go leg's goldens test).
func csHeldUnitSchemas(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, u := range csHeldUnits {
		out = append(out, csHeldSchemaFor(t, u))
	}
	return out
}

// csHeldSchemaFor reads one unit's schemas as one source: the unit line names
// either a directory (its *.schema files) or one file, and the per-file
// `package` lines are folded into the one the unit declares (the Go leg's
// parseManifest/loadUnitPaths rule).
func csHeldSchemaFor(t *testing.T, u csHeldUnit) string {
	t.Helper()
	base := filepath.Join("..", "..", "..", u.dir)
	info, err := os.Stat(base)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if info.IsDir() {
		paths, err = filepath.Glob(filepath.Join(base, "*.schema"))
		if err != nil {
			t.Fatal(err)
		}
	} else {
		paths = []string{base}
	}
	if len(paths) == 0 {
		t.Fatalf("no schemas under %s", u.dir)
	}
	src := "package " + u.pkg + "\n"
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		part := strings.ReplaceAll(string(b), "package "+u.pkg, "")
		// The pointer corpus carries a C++ native mapping the C# leg has no
		// use for; as a single probe file it cannot ride, so it is dropped.
		part = strings.ReplaceAll(part, ` | cpp_native = ColourMath, cpp_include = "graph_colour.h"`, "")
		src += part + "\n"
	}
	return src
}

// csHeldCompose is the source the compiler is handed: the clause harness with
// the unit aliases spliced in and the manifest-row class appended.
func csHeldCompose(t *testing.T) string {
	t.Helper()
	src := strings.Replace(csHeldMain, "using Probe;\n", "using Probe;\n"+csHeldAliases(), 1)
	return src + "\n" + csHeldRowsClass(t)
}

func csHeldAliases() string {
	var b strings.Builder
	for _, u := range csHeldUnits {
		fmt.Fprintf(&b, "using %ss = %s.Schema;\n", u.pre, u.ns)
		fmt.Fprintf(&b, "using %sv = %s.TableVocabulary;\n", u.pre, u.ns)
		fmt.Fprintf(&b, "using %sr = %s.TableReport;\n", u.pre, u.ns)
	}
	return b.String()
}

func csHeldPre(pkg string) string {
	for _, u := range csHeldUnits {
		if u.pkg == pkg {
			return u.pre
		}
	}
	return ""
}

func csHeldNS(pkg string) string {
	for _, u := range csHeldUnits {
		if u.pkg == pkg {
			return u.ns
		}
	}
	return ""
}

func csHeldRowsClass(t *testing.T) string {
	t.Helper()
	corpus, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "wire", "tables"))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`static class Rows
{
    static int bad = 0;
    static readonly string corpus = @"` + corpus + `";
    static void Ok(string row) { Console.WriteLine("ROW row/" + row + ": ok"); }
    static void Fail(string row, string what) { Console.WriteLine("ROW row/" + row + ": FAILED " + what); bad++; }
    static bool Same(byte[] a, byte[] b)
    {
        if (a.Length != b.Length) { return false; }
        for (int i = 0; i < a.Length; ++i) { if (a[i] != b[i]) { return false; } }
        return true;
    }
    static byte[] Corpus(string f) { return System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, f)); }

    public static int Run()
    {
        Console.WriteLine("row harness: " + ` + fmt.Sprintf("%d", len(csHeldConns)+len(csHeldMsgs)) + ` + " rows");
`)
	for _, conn := range csHeldConns {
		b.WriteString(csHeldConnMethod(conn))
		b.WriteString("        Conn_" + conn.key + "();\n")
	}
	for _, msg := range csHeldMsgs {
		b.WriteString(csHeldMsgMethod(msg))
		b.WriteString("        Msg_" + msg.name + "();\n")
	}
	b.WriteString("    return bad;\n}\n}\n")
	return b.String()
}

func csHeldConnMethod(c csHeldConn) string {
	p := csHeldPre(c.pkg)
	return fmt.Sprintf(`    static void Conn_%s()
    {
        const string row = %q;
        byte[] announcement = %ss.Announce().ToArray();
        if (%ss.AnnounceMeasure() != announcement.Length) { Fail(row, "announce measure " + %ss.AnnounceMeasure() + " != " + announcement.Length); return; }
        if (!Same(announcement, Corpus(%q))) { Fail(row, "the announcement bytes differ from the manifest wire"); return; }
        var vocab = new %sv();
        var report = new %sr();
        %ss.AnnounceRead(vocab, Corpus(%q), report);
        if (!vocab.Announced || report.Refused || report.Malformed) { Fail(row, "announce: " + report.Reason); return; }
        if (vocab.BuildVersion != %sUL) { Fail(row, "build version " + vocab.BuildVersion.ToString("x16")); return; }
        Ok(row);
    }
`, c.key, c.key, p, p, p, c.wire, p, p, p, c.wire, c.version)
}

func csHeldMsgMethod(m csHeldMsg) string {
	p, ns := csHeldPre(m.pkg), csHeldNS(m.pkg)
	rootType := ns + "." + m.root
	return fmt.Sprintf(`    static void Msg_%s()
    {
        const string row = %q;
        byte[] file = Corpus(%q);
        byte[] msg = Corpus(%q);
        var value = new %s();
        var loadReport = new %sr();
        if (!%ss.%sLoad(value, file, loadReport) || loadReport.Refused || loadReport.Malformed) { Fail(row, "file load: " + loadReport.Reason); return; }
        var values = new %s[] { value };
        long measure = %ss.%sMeasureMessages(values);
        byte[] outMsg = new byte[measure];
        var saveReport = new %sr();
        if (%ss.%sSaveMessages(values, outMsg, saveReport) != measure || !Same(outMsg, msg)) { Fail(row, "file->message bytes differ"); return; }
        var vocab = new %sv();
        var annReport = new %sr();
        %ss.AnnounceRead(vocab, Corpus(%q), annReport);
        if (!vocab.Announced) { Fail(row, "announce: " + annReport.Reason); return; }
        var storage = new %s[] { new %s() };
        var msgReport = new %sr();
        %ss.%sLoadMessages(storage, msg, vocab, msgReport, out int count);
        if (count != 1 || msgReport.Refused || msgReport.Malformed) { Fail(row, "message load: count=" + count + " " + msgReport.Reason); return; }
        long fileMeasure = %ss.%sMeasure(storage[0]);
        byte[] outFile = new byte[fileMeasure];
        if (%ss.%sSave(storage[0], outFile) != fileMeasure || !Same(outFile, file)) { Fail(row, "message->file bytes differ"); return; }
        Ok(row);
    }
`, m.name, m.name, m.file, m.msg, rootType, p, p, m.root, rootType, p, m.root, p, p, m.root, p, p, p, m.conn, rootType, rootType, p, p, m.root, p, m.root, p, m.root)
}
