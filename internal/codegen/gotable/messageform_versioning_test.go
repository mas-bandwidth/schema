package gotable

// THE GO MESSAGE FORM'S VERSIONING SURFACE (docs/SPEC-TABLES.md §4, §3.3,
// §20). The sprint's client and server speak the message form across releases,
// so a version mismatch must be DETECTABLE by the application and every
// tolerated difference reported exactly. §4 makes the wire the versioning
// model: an unknown field is skipped by its length and counted, an absent field
// takes its declared default, a widened kind decodes exactly and counts, and
// every other kind change is `kind_mismatch` and never a silent wrong value.
// §3.3 makes closing a connection the application's act — "The library RETURNS
// A REFUSAL and nothing more" — so this card proves the Go surface hands an
// application everything it needs to refuse a mismatched endpoint and decides no
// new refusal. §20's build version is the public fact that says WHICH schema a
// endpoint announced, and it is compiler-settled so an application compares
// against it with no parsing of its own.
//
// Every subtest generates TWO units of one fixed table — an OLDER and a NEWER
// schema — and drives the older unit's Announce and SaveMessages into the
// newer unit's AnnounceRead and LoadMessages, exactly as an endpoint pair would.
// A fixed table carries the message form (§3.3); a plain table may not.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runMessagePair generates the WRITER unit and the READER unit into one module
// (two directories, both declaring the schema's `package endpoint`, so a same-schema
// pair carries ONE build version) and runs a probe that announces the writer's
// vocabulary to the reader, saves one writer body into a batch, and loads it
// with the reader. `setup` fills the writer's value; `asserts` holds
// the public-surface assertions. Nothing here is a new library verb: the probe
// is the application.
func runMessagePair(t *testing.T, writerSrc, readerSrc, setup, asserts string) {
	t.Helper()
	if out, err := runMessagePairEdited(t, writerSrc, readerSrc, setup, asserts, nil); err != nil {
		t.Fatalf("message endpoint probe: %v\n%s", err, out)
	}
}

// runMessagePairEdited is runMessagePair with an edit applied to the READER's
// generated files, and the run's output and error returned rather than judged:
// a negative control plants a fault in the reader through it and requires red.
func runMessagePairEdited(t *testing.T, writerSrc, readerSrc, setup, asserts string, editReader func(map[string][]byte)) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	reader := generatedPackage(t, readerSrc)
	if editReader != nil {
		editReader(reader)
	}
	writePackageDir(t, filepath.Join(dir, "oldpeer"), generatedPackage(t, writerSrc))
	writePackageDir(t, filepath.Join(dir, "newpeer"), reader)
	src := fmt.Sprintf(`package probe

import (
	"testing"

	old "probe/oldpeer"
	new "probe/newpeer"
)

func TestRemote(t *testing.T) {
	var w old.Root
	old.RootReset(&w)
	%s
	values := []*old.Root{&w}
	size := old.RootMeasureMessages(values, nil)
	if size < 2 {
		t.Fatalf("measure messages: %%d", size)
	}
	out := make([]byte, size)
	if old.RootSaveMessages(values, out, nil) != int64(len(out)) {
		t.Fatal("save messages")
	}
	announcement := make([]byte, old.AnnounceMeasure())
	if old.Announce(announcement) != int64(len(announcement)) {
		t.Fatal("announce")
	}
	var storage [64]new.TableMessageEntry
	var vocab new.TableVocabulary
	vocab.Init(storage[:])
	var arep new.TableReport
	if !new.AnnounceRead(&vocab, announcement, &arep) {
		t.Fatalf("the reader refused the writer's announcement: %%+v", arep)
	}
	decoded := make([]new.Root, 1)
	var report new.TableReport
	count, ok := new.RootLoadMessages(decoded, &vocab, out, &report)
	_ = count
	_ = ok
%s
}
`, setup, asserts)
	runtime := slowtest.SerializeGo(t, "../../../")
	root := map[string][]byte{
		"go.mod":          []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)),
		"version_test.go": []byte(src),
	}
	writeFiles(t, dir, root)
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated endpoint pair)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// generatedPackage is one unit's generated Go sources, tables and packet
// classes together, exactly as runGeneratedUnit merges them.
func generatedPackage(t *testing.T, src string) map[string][]byte {
	t.Helper()
	u := unitFrom(t, src)
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return files
}

func writePackageDir(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, files)
}

func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The detect-and-refuse recipe §4 and §3.3 hand the APPLICATION, spelled once
// and copied into every probe: an endpoint whose announced build version is not this
// build's, or a batch that reported any tolerance event, is an endpoint to refuse.
// It is computed from the public surface alone and adds no library verb.
const detectRecipe = `	detect := func() bool {
		return vocab.BuildVersion != new.BuildVersion ||
			report.Unknown != 0 || report.KindMismatch != 0 ||
			report.Widened != 0 || report.Clamped != 0 ||
			report.Duplicate != 0 || report.Malformed
	}`

const sameSchema = `package endpoint

fixed table Root
{
	a int32
	b int16
}
`

const sameSchemaReader = `package endpoint

fixed table Root
{
	a int32
	b int16
}
`

const newerWriter = `package endpoint

fixed table Root
{
	a     int32
	b     int16
	extra int32
}
`

const newerReader = `package endpoint

fixed table Root
{
	a int32
	b int16
}
`

const olderWriter = `package endpoint

fixed table Root
{
	a int32
	b int16
}
`

const olderReader = `package endpoint

fixed table Root
{
	a     int32
	b     int16
	later int32 = 7
}
`

const widenedWriter = `package endpoint

fixed table Root
{
	a int32
	b int16
	c int32
}
`

const widenedReader = `package endpoint

fixed table Root
{
	a int32
	b int32
	c float32
}
`

func TestGoMessageFormVersioning(t *testing.T) {
	t.Parallel()

	// 2.1 §20: "The BUILD VERSION is everything cooked or blocked" and §3.3
	// announces it once, before the first body. The endpoint's announced build
	// version is readable after AnnounceRead, equals the writer unit's own
	// generated constant, and survives a refused announcement because §3.3
	// keeps the refusal a state and not a lost parse.
	t.Run("peer_build_version", func(t *testing.T) {
		t.Parallel()
		runMessagePair(t, newerWriter, newerReader, "\tw.A = 11\n\tw.B = -5\n\tw.Extra = 99", `
	if vocab.BuildVersion != old.BuildVersion {
		t.Fatalf("the announced build version is not the writer's own: vocab=%#x writer=%#x", vocab.BuildVersion, old.BuildVersion)
	}
	if vocab.BuildVersion == new.BuildVersion {
		t.Fatalf("two different schemas announce one build version: %#x", vocab.BuildVersion)
	}
	var refused new.TableReport
	if new.AnnounceRead(&vocab, announcement, &refused) {
		t.Fatalf("a second announcement was not refused: %+v", refused)
	}
	if refused.Reason != "second_announcement" {
		t.Fatalf("the refusal is not by name: %+v", refused)
	}
	if vocab.BuildVersion != old.BuildVersion {
		t.Fatalf("a refused announcement lost the endpoint's build version: %#x", vocab.BuildVersion)
	}
`)
	})

	// 2.2 §20.7: the build version "rides in the TABLE-bearing sources", so an
	// application that uses the MESSAGE form alone — no block runtime and no
	// cook runtime — still holds the reader's own constant to compare against.
	t.Run("own_build_version_without_accelerators", func(t *testing.T) {
		t.Parallel()
		if out, err := runGeneratedEdited(t, `package probe

fixed table Root
{
	n int32
}
`, `package probe

import "testing"

func TestOwnBuildVersion(t *testing.T) {
	if BuildVersion == 0 {
		t.Fatal("a message-bearing unit exposes no own build version")
	}
}
`, func(files map[string][]byte) {
			for name := range files {
				if strings.HasSuffix(name, "Block.go") || strings.HasSuffix(name, "Cook.go") {
					delete(files, name)
				}
			}
		}); err != nil {
			t.Fatalf("generated runtime: %v\n%s", err, out)
		}
	})

	// 2.2 §20.7, the case the block-formable schema above hides: a FIXED TABLE
	// that carries the message form but has NO block form — a union fills the
	// closure and §19.3 gives it no block — must still expose its own build
	// version, because §3.3's Announce is in the TABLE source and an
	// application compares against the constant without a cook runtime. It is
	// red when the constant is emitted only with the cook runtime.
	t.Run("own_build_version_with_unblockable_table", func(t *testing.T) {
		t.Parallel()
		if out, err := runGeneratedEdited(t, `package probe

fixed table Root
{
	a int32
	u U
}

union U
{
	x int32
	y int32
}
`, `package probe

import "testing"

func TestOwnBuildVersion(t *testing.T) {
	if BuildVersion == 0 {
		t.Fatal("a message-bearing fixed table with no block form exposes no own build version")
	}
}
`, func(files map[string][]byte) {
			for name := range files {
				if strings.HasSuffix(name, "Block.go") || strings.HasSuffix(name, "Cook.go") {
					delete(files, name)
				}
			}
		}); err != nil {
			t.Fatalf("generated runtime: %v\n%s", err, out)
		}
	})

	// 2.3 §4: "Unknown field (newer writer): skipped by its length, counted."
	// A newer writer's field the reader does not have reads clean, the field is
	// stepped over and counted `unknown`, and the known fields keep the writer's
	// values.
	t.Run("newer_writer_unknown_field", func(t *testing.T) {
		t.Parallel()
		runMessagePair(t, newerWriter, newerReader, "\tw.A = 11\n\tw.B = -5\n\tw.Extra = 99", `
	if !ok {
		t.Fatalf("a newer writer's batch was refused: %+v", report)
	}
	if report.Unknown != 1 {
		t.Fatalf("the newer field is not counted once as unknown: %+v", report)
	}
	if report.KindMismatch != 0 || report.Widened != 0 || report.Clamped != 0 || report.Duplicate != 0 || report.Malformed {
		t.Fatalf("a skipped known kind moved another counter: %+v", report)
	}
	if decoded[0].A != 11 || decoded[0].B != -5 {
		t.Fatalf("a known field lost the writer's value: %+v", decoded[0])
	}
`+detectRecipe+`
	if !detect() {
		t.Fatalf("the detect-and-refuse recipe missed a newer writer: %+v", report)
	}
`)
	})

	// 2.4 §4: "Absent field (older writer): the reader's value takes the
	// field's default." The reader's field the older writer never sent reads
	// at its declared default with the report silent.
	t.Run("older_writer_absent_field", func(t *testing.T) {
		t.Parallel()
		runMessagePair(t, olderWriter, olderReader, "\tw.A = 11\n\tw.B = -5", `
	if !ok {
		t.Fatalf("an older writer's batch was refused: %+v", report)
	}
	if report.Unknown != 0 || report.KindMismatch != 0 || report.Widened != 0 || report.Clamped != 0 || report.Duplicate != 0 || report.Malformed {
		t.Fatalf("an absent field is silent in §4's counters: %+v", report)
	}
	if decoded[0].Later != 7 {
		t.Fatalf("the absent field is not its declared default 7: %+v", decoded[0])
	}
	if decoded[0].A != 11 || decoded[0].B != -5 {
		t.Fatalf("an older writer's known fields did not land: %+v", decoded[0])
	}
`+detectRecipe+`
	if !detect() {
		t.Fatalf("the detect-and-refuse recipe missed an older writer's build version: %+v", report)
	}
`)
	})

	// 2.5 §4: a widened kind "decodes EXACTLY, the value lands, and one
	// `widened` counts"; a kind change the silent class does not admit is a
	// `kind_mismatch`, "skipped, never misdecoded, counted", so no wrong value
	// is ever produced.
	t.Run("widened_and_kind_mismatch", func(t *testing.T) {
		t.Parallel()
		runMessagePair(t, widenedWriter, widenedReader, "\tw.A = 1\n\tw.B = -5\n\tw.C = 9", `
	// the plain LoadMessages is strict (docs/SPEC-TABLES.md §6.5) and refuses
	// the kind mismatch; the tolerant read is the one that counts it
	if ok || report.Err() != error(new.TableRefuseKindMismatch) {
		t.Fatalf("the plain read took a kind-mismatched batch: %+v", report)
	}
	report = new.TableReport{}
	count, ok = new.RootLoadMessagesTolerant(decoded, &vocab, out, &report)
	if !ok {
		t.Fatalf("a widening writer's batch was refused: %+v", report)
	}
	if report.Widened != 1 {
		t.Fatalf("the int16-to-int32 widening did not count once: %+v", report)
	}
	if decoded[0].B != -5 {
		t.Fatalf("the widened value is not exact: %+v", decoded[0])
	}
	if report.KindMismatch != 1 {
		t.Fatalf("the int32-to-float32 change did not count as kind_mismatch: %+v", report)
	}
	if decoded[0].C != 0 {
		t.Fatalf("a kind-mismatched field was misdecoded rather than defaulted: %+v", decoded[0])
	}
	if report.Unknown != 0 || report.Clamped != 0 || report.Duplicate != 0 || report.Malformed {
		t.Fatalf("a widening and a mismatch moved another counter: %+v", report)
	}
`+detectRecipe+`
	if !detect() {
		t.Fatalf("the detect-and-refuse recipe missed a widening writer: %+v", report)
	}
`)
	})

	// 2.6 §4 and §3.3: the detect-and-refuse recipe is an assertion an
	// application writes for itself, and it is FALSE for an endpoint whose schema is
	// this build's: the announced build version equals this build's own and the
	// report is silent.
	t.Run("same_schema_peer_is_not_refused", func(t *testing.T) {
		t.Parallel()
		runMessagePair(t, sameSchema, sameSchemaReader, "\tw.A = 11\n\tw.B = -5", `
	if !ok {
		t.Fatalf("a same-schema endpoint was refused: %+v", report)
	}
	if report.Unknown != 0 || report.KindMismatch != 0 || report.Widened != 0 || report.Clamped != 0 || report.Duplicate != 0 || report.Malformed {
		t.Fatalf("a same-schema read is silent: %+v", report)
	}
	if decoded[0].A != 11 || decoded[0].B != -5 {
		t.Fatalf("a same-schema endpoint's values did not land: %+v", decoded[0])
	}
`+detectRecipe+`
	if detect() {
		t.Fatalf("the detect-and-refuse recipe fired on a same-schema endpoint: %+v", report)
	}
`)
	})

	// 2.7 §4: "The wire itself is evolution-tolerant, and that tolerance is the
	// versioning model." The tolerant read is the default: an endpoint of a
	// different build is REPORTED, never refused by the library. The opt-in
	// strict mode is the card schema-go-msg-strict, after this one, and nothing
	// about it is asserted here.
	t.Run("tolerant_read_is_the_default", func(t *testing.T) {
		t.Parallel()
		runMessagePair(t, newerWriter, newerReader, "\tw.A = 11\n\tw.B = -5\n\tw.Extra = 99", `
	if !ok {
		t.Fatalf("the tolerant read refused an endpoint of a different build: %+v", report)
	}
	if report.Verdict != new.TableOpenOk || report.Reason != "" {
		t.Fatalf("the library raised a refusal where §3.3 leaves the choice to the application: %+v", report)
	}
	if decoded[0].A != 11 || decoded[0].B != -5 {
		t.Fatalf("the tolerant read lost a known field: %+v", decoded[0])
	}
`)
	})
}
