package ctable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormHeldC asserts, one subtest per clause, the refusals and
// reports the MESSAGE FORM names on the generated C surface
// (docs/SPEC-TABLES.md §3.3). Each clause drives a unit whose root is a
// `fixed table` — the form is a fixed table's (§3.3) — and every assertion
// quotes the sentence it implements. The rows of
// testdata/conformance/tables/MANIFEST.txt pin the reference bytes below.
func TestMessageFormHeldC(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
 a int32
 b bool
 c uint8
}
`
	clauses := []struct {
		name  string
		probe string
	}{
		{"no_vocabulary", cMsgNoVocabularyProbe},
		{"second_announcement", cMsgSecondAnnouncementProbe},
		{"vocabulary_too_large", cMsgVocabularyTooLargeProbe},
		{"announcement_strict_checks", cMsgAnnouncementStrictProbe},
		{"batch_too_large", cMsgBatchTooLargeProbe},
		{"damage_inside_body", cMsgDamageInsideBodyProbe},
		{"bytes_after_pad", cMsgBytesAfterPadProbe},
		{"form_byte", cMsgFormByteProbe},
	}
	cMessageManifestRows(t)
	for _, clause := range clauses {
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			runCGenerated(t, schema, clause.probe)
		})
	}
}

// cMsgShared is the vocabulary a probe opens: the unit's own announcement and
// the caller's resolved storage (docs/SPEC-TABLES.md §3.3).
const cMsgShared = `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){ fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
`

// "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
// decoded, no counter moves and `malformed` does not fire." (docs/SPEC-TABLES.md §3.3).
const cMsgNoVocabularyProbe = cMsgShared + `
int main(void)
{
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    uint8_t wire[] = { 2, 0, 0 };
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 0);
    CHECK(report.refused && !report.malformed && report.reason == SCHEMA_TABLE_NO_VOCABULARY);
    return 0;
}
`

// "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does not
// replace or amend anything." and "REFUSAL IS TERMINAL ... every announcement
// after it is refused as `second_announcement` ... every body after it is
// refused for want of one." (docs/SPEC-TABLES.md §3.3).
const cMsgSecondAnnouncementProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport first; memset(&first, 0, sizeof(first));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &first));
    CHECK(v.announced && v.count == kTableMessageEntriesHere);
    int64_t before = v.count;
    TableReport second; memset(&second, 0, sizeof(second));
    CHECK(!announce_read(&v, ann, (int64_t)sizeof(ann), &second));
    CHECK(second.refused && !second.malformed && second.reason == SCHEMA_TABLE_SECOND_ANNOUNCEMENT);
    CHECK(v.count == before);
    /* a refused FIRST announcement is terminal: every announcement after it is
       refused as second_announcement, and every body is refused for want of a
       vocabulary. */
    TableMessageEntry e2[kTableMessageEntriesHere];
    TableVocabulary w = table_vocabulary(e2, kTableMessageEntriesHere);
    uint8_t bad[] = { 9, 9, 9 };
    TableReport rejected; memset(&rejected, 0, sizeof(rejected));
    CHECK(!announce_read(&w, bad, (int64_t)sizeof(bad), &rejected));
    CHECK(rejected.refused && rejected.reason == SCHEMA_TABLE_NEWER_FORM && !w.announced);
    TableReport later; memset(&later, 0, sizeof(later));
    CHECK(!announce_read(&w, ann, (int64_t)sizeof(ann), &later));
    CHECK(later.reason == SCHEMA_TABLE_SECOND_ANNOUNCEMENT && !w.announced);
    uint8_t wire[] = { 2, 0, 0 };
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport body; memset(&body, 0, sizeof(body));
    CHECK(!root_load_messages(storage, &count, &w, wire, (int64_t)sizeof(wire), &body));
    CHECK(count == 0 && body.reason == SCHEMA_TABLE_NO_VOCABULARY);
    return 0;
}
`

// "`vocabulary_too_large` covers both bounds, the entry count and the
// vocabulary's bytes." (docs/SPEC-TABLES.md §3.3).
const cMsgVocabularyTooLargeProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    /* the entry count past the caller's storage */
    TableMessageEntry small[1];
    TableVocabulary v = table_vocabulary(small, 1);
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!announce_read(&v, ann, (int64_t)sizeof(ann), &report));
    CHECK(report.refused && !report.malformed && report.reason == SCHEMA_TABLE_VOCABULARY_TOO_LARGE && !v.announced);
    /* the vocabulary bytes past MaxBytes */
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary b = table_vocabulary(entries, kTableMessageEntriesHere);
    b.max_bytes = 4;
    TableReport br; memset(&br, 0, sizeof(br));
    CHECK(!announce_read(&b, ann, (int64_t)sizeof(ann), &br));
    CHECK(br.refused && !br.malformed && br.reason == SCHEMA_TABLE_VOCABULARY_TOO_LARGE && !b.announced);
    return 0;
}
`

// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS: the build
// version present, exactly once, kind `9`, eight bytes wide, and the vocabulary
// present, exactly once, kind `14`, element kind `6` ... A FAILED STRICT CHECK
// IS MALFORMED, not a refusal ... The build version is kept the moment it is
// read, refusal or not." (docs/SPEC-TABLES.md §3.3).
const cMsgAnnouncementStrictProbe = cMsgShared + `
static size_t put_lebs(uint8_t * out, uint64_t v)
{
    size_t n = 0;
    for(;;) { uint8_t c = (uint8_t)(v & 0x7f); v >>= 7; if(v) c |= 0x80; out[n++] = c; if(!v) return n; }
}
static size_t put_le64(uint8_t * out, uint64_t v)
{
    int i; for(i = 0; i < 8; i++) out[i] = (uint8_t)(v >> (8 * i)); return 8;
}
static size_t version_field(uint8_t * out, uint64_t v)
{
    out[0] = 1; out[1] = 9; put_le64(out + 2, v); return 10;
}
static size_t vocab_field(uint8_t * out, uint8_t elem, const uint8_t * vocab, size_t len)
{
    uint8_t sub[64]; size_t sn = 0, n = 2;
    sub[sn++] = elem; sn += put_lebs(sub + sn, (uint64_t)len);
    if(len) { memcpy(sub + sn, vocab, len); } sn += len;
    out[0] = 2; out[1] = 14; n += put_lebs(out + n, (uint64_t)sn);
    memcpy(out + n, sub, sn); return n + sn;
}
static size_t announcement(uint8_t * out, const uint8_t * fields, size_t fn)
{
    size_t n = 0;
    out[n++] = 1; if(fn) memcpy(out + n, fields, fn); n += fn; out[n++] = 0x00;
    n += put_le64(out + n, UINT64_C(0xfffffffffffffffe));
    n += put_le64(out + n, UINT64_C(0xfffffffffffffffd));
    n += put_le64(out + n, 2);
    return n;
}
static int refusal_is_malformed(const uint8_t * data, size_t bytes)
{
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport report; memset(&report, 0, sizeof(report));
    int ok = announce_read(&v, data, (int64_t)bytes, &report);
    return !ok && report.malformed && !report.refused && !v.announced;
}
int main(void)
{
    const uint64_t version = UINT64_C(0x1122334455667788);
    uint8_t fields[256], out[512], twice[64]; size_t fn, n;
    /* build version missing */
    fn = 0; fn += vocab_field(fields + fn, 6, NULL, 0);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* build version twice */
    fn = 0; fn += version_field(fields + fn, version); fn += version_field(fields + fn, version); fn += vocab_field(fields + fn, 6, NULL, 0);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* vocabulary missing */
    fn = 0; fn += version_field(fields + fn, version);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* vocabulary not kind 14 */
    fn = 0; fn += version_field(fields + fn, version);
    fields[fn++] = 2; fields[fn++] = 13;
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* vocabulary element not kind 6 */
    fn = 0; fn += version_field(fields + fn, version); fn += vocab_field(fields + fn, 9, NULL, 0);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* the build version is kept the moment it is read, refusal or not */
    (void)twice; (void)n;
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
        fn = 0; fn += version_field(fields + fn, version);
        fields[fn++] = 2; fields[fn++] = 13;
        size_t bytes = announcement(out, fields, fn);
        TableReport report; memset(&report, 0, sizeof(report));
        CHECK(!announce_read(&v, out, (int64_t)bytes, &report));
        CHECK(report.malformed && v.build_version == version && !v.announced);
    }
    return 0;
}
`

// "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... `MeasureMessages`
// and `SaveMessages` both refuse and return `-1`" and "`M` ABOVE THE CALLER'S
// CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... The reader sets the
// returned count to the wire's `M` before it returns the refusal."
// (docs/SPEC-TABLES.md §3.3).
const cMsgBatchTooLargeProbe = cMsgShared + `
int main(void)
{
    Root values[257]; int i;
    for(i = 0; i < 257; i++) root_reset(values + i);
    TableReport measure; memset(&measure, 0, sizeof(measure));
    CHECK(root_measure_messages(values, 257, &measure) == -1);
    CHECK(measure.refused && measure.reason == SCHEMA_TABLE_BATCH_TOO_LARGE);
    uint8_t buf[16];
    TableReport save; memset(&save, 0, sizeof(save));
    CHECK(root_save_messages(values, 257, buf, (int64_t)sizeof(buf), &save) == -1);
    CHECK(save.refused && save.reason == SCHEMA_TABLE_BATCH_TOO_LARGE);
    /* the read side: two bodies into storage for one */
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    uint8_t wire[] = { 2, 1, 0 }; /* count minus one is 1: M is two */
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 2 && report.refused && !report.malformed && report.reason == SCHEMA_TABLE_BATCH_TOO_LARGE);
    CHECK(storage[0].a == 0 && storage[0].b == 0 && storage[0].c == 0);
    return 0;
}
`

// "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`. The returned count
// states `k - 1`, ONE `malformed` counts, and nothing at or after body `k` is
// read." (docs/SPEC-TABLES.md §3.3).
const cMsgDamageInsideBodyProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    /* M is three: body 1's reference is 0, body 2's first reference is 0xF,
       above the announced entry count E = 8, and body 3 is never read. A
       reference is four bits, low bit first, so byte 1 holds body 1's low
       nibble and body 2's high nibble. */
    uint8_t wire[] = { 2, 2, 0xF0 };
    Root storage[3]; int i;
    for(i = 0; i < 3; i++) root_reset(storage + i);
    int64_t count = 3;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 1);
    CHECK(report.malformed && !report.refused);
    return 0;
}
`

// "BYTES AFTER THE PAD ARE MALFORMED ... the batch ends at the pad to the byte
// boundary, and a buffer with bytes left over describes no batch this reader
// can name." (docs/SPEC-TABLES.md §3.3).
const cMsgBytesAfterPadProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    uint8_t wire[] = { 2, 0, 0, 0 }; /* a valid one-body batch, one byte over */
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 1 && report.malformed && !report.refused);
    return 0;
}
`

// "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does not
// know is a REFUSAL by name and never damage." and "a reader handed a form-`2`
// wire where a file was expected refuses by name."
// (docs/SPEC-TABLES.md §3.3).
const cMsgFormByteProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    uint8_t form1[] = { 1 };
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages(storage, &count, &v, form1, (int64_t)sizeof(form1), &report));
    CHECK(count == 0 && report.refused && !report.malformed && report.reason == SCHEMA_TABLE_NEWER_FORM);
    /* a form-2 batch handed to the FILE reader -- the announcement's form-1
       reader -- refuses by name and moves no counter. */
    TableMessageEntry e2[kTableMessageEntriesHere];
    TableVocabulary w = table_vocabulary(e2, kTableMessageEntriesHere);
    uint8_t form2[] = { 2, 0, 0 };
    TableReport r2; memset(&r2, 0, sizeof(r2));
    CHECK(!announce_read(&w, form2, (int64_t)sizeof(form2), &r2));
    CHECK(r2.refused && !r2.malformed && r2.reason == SCHEMA_TABLE_MESSAGE_FORM_AS_FILE);
    return 0;
}
`

// cMessageManifestRows holds the announcement and batch bytes of §3.3 against
// every connection and message row the conformance manifest names.
func cMessageManifestRows(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units, connections := map[string]string{}, map[string][]string{}
	var rows [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "unit":
			units[f[1]] = f[2]
		case "connection":
			connections[f[1]] = f
			rows = append(rows, f)
		case "message":
			rows = append(rows, f)
		}
	}
	for _, row := range rows {
		t.Run(row[0]+"/"+row[1], func(t *testing.T) {
			t.Parallel()
			connection := row
			if row[0] == "message" {
				connection = connections[row[2]]
			}
			path := filepath.Join(root, units[connection[2]])
			paths := []string{path}
			if !strings.HasSuffix(path, ".schema") {
				var err error
				paths, err = filepath.Glob(filepath.Join(path, "*.schema"))
				if err != nil {
					t.Fatal(err)
				}
			}
			var sources []check.SourceFile
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				ast, diags := parser.Parse(path, data)
				if len(diags) > 0 {
					t.Fatal(diags)
				}
				name := filepath.Base(path)
				sources = append(sources, check.SourceFile{Path: path, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: data, AST: ast})
			}
			unit, diags := check.Unit(sources)
			if len(diags) > 0 {
				t.Fatal(diags)
			}
			expected, err := os.ReadFile(filepath.Join(root, connection[4]))
			if err != nil {
				t.Fatal(err)
			}
			source := "#include <stdint.h>\n#include <stdlib.h>\n#include <string.h>\n"
			files, err := Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			for name := range files {
				if strings.HasSuffix(name, "Table.h") {
					source += fmt.Sprintf("#include %q\n", name)
				}
			}
			source += fmt.Sprintf("static const uint8_t expected[] = {%s};\n", cMessageBytes(expected))
			if row[0] == "connection" {
				// "The announcement is a compile-time constant byte array with a length." (§3.3)
				source += "int main(void){uint8_t out[sizeof(expected)];return announce(out,sizeof(out))!=sizeof(expected)||memcmp(out,expected,sizeof(out));}\n"
			} else {
				wire, err := os.ReadFile(filepath.Join(root, row[5]))
				if err != nil {
					t.Fatal(err)
				}
				source += fmt.Sprintf("static const uint8_t wire[] = {%s};\n", cMessageBytes(wire))
				fn := ""
				for i, r := range row[3] {
					if r >= 'A' && r <= 'Z' {
						if i > 0 {
							fn += "_"
						}
						fn += strings.ToLower(string(r))
					} else {
						fn += string(r)
					}
				}
				source += "int main(void){TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};int64_t count=1;uint8_t out[sizeof(wire)];if(!announce_read(&v,expected,sizeof(expected),&report))return 1;"
				if row[1] == "graph_tree" {
					source += fmt.Sprintf("const %s * values[1]={0};int64_t need=%s_load_measure_messages(&v,wire,sizeof(wire));if(need<0)return 2;uint8_t * region=malloc((size_t)need);if(!region)return 3;if(!%s_load_messages(values,&count,region,need,&v,wire,sizeof(wire),&report))return 4;", row[3], fn, fn)
				} else {
					source += fmt.Sprintf("%s values[1];if(!%s_load_messages(values,&count,&v,wire,sizeof(wire),&report))return 4;", row[3], fn)
				}
				// "A batch of bodies is the unit this form reads and writes." (§3.3)
				source += fmt.Sprintf("if(count!=1||%s_measure_messages(values,count,&report)!=sizeof(wire)||%s_save_messages(values,count,out,sizeof(out),&report)!=sizeof(wire))return 5;return memcmp(out,wire,sizeof(wire));}\n", fn, fn)
			}
			runCMessageUnit(t, unit, source)
		})
	}
}

// cMessageBytes embeds the reference bytes without changing their pins (§3.3).
func cMessageBytes(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprint(b)
	}
	return strings.Join(parts, ",")
}

// runCMessageUnit executes the generated C announcement and batch surface (§3.3).
func runCMessageUnit(t *testing.T, u *ir.Unit, source string) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal("C message conformance requires cc")
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O2", "-I", dir, main}
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	binary := filepath.Join(dir, "probe")
	args = append(args, "-lm", "-o", binary)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("§3.3 reference bytes differ: %v\n%s", err, out)
	}
}
