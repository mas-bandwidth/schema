// TestCRunsHostileCorpus is the C port's runner over the SHARED HOSTILE INPUT
// CORPUS (docs/HOSTILE-CORPUS.md; schema-shared-hostile-corpus). It reads the
// committed manifest and its raw inputs, generates the C table reader for each
// schema the manifest names, compiles it in a scratch directory, and holds the
// port to the manifest: the same verdict, the same named reason, and for an
// accept the same SHA-256 of the engine's canonical text form.
//
// The runner reads files only: no network, no real time, no sockets. A mismatch
// is a finding, reported as FIX-CARD with the input's name.
package ci_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	codegenc "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ctable"
)

// hostileProbeSchema is the one schema the corpus keeps out of the checkout,
// copied from internal/ci/hostilecorpus so this runner needs no package the
// corpus card owns (docs/HOSTILE-CORPUS.md, "Carriers").
const hostileProbeSchema = "package probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

type cHostileCase struct {
	name    string
	carrier string
	schema  string
	table   string
	input   string
	verdict string
	reason  string
	digest  string
}

const cHostileFindingPrefix = "FIX-CARD: c-hostile-corpus: "

// Keep the carried reader mismatches visible while making both repair and
// regression observable: clearing an expected mismatch or adding another one
// changes this exact set and turns the unit gate red.
var cExpectedHostileMismatches = []string{
	"conformance-label-type-mismatch",
	"conformance-pointer-array-shared-label",
	"conformance-shared-node-by-label",
}

func cHostileMismatchSetDrift(findings, expected []string) []string {
	got := make([]string, 0, len(findings))
	for _, finding := range findings {
		name := "<unclassified>"
		if strings.HasPrefix(finding, cHostileFindingPrefix) {
			fields := strings.Fields(strings.TrimPrefix(finding, cHostileFindingPrefix))
			if len(fields) > 0 {
				name = fields[0]
			}
		}
		got = append(got, name)
	}
	got = append([]string(nil), got...)
	want := append([]string(nil), expected...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) == len(want) {
		equal := true
		for i := range got {
			if got[i] != want[i] {
				equal = false
				break
			}
		}
		if equal {
			return nil
		}
	}
	return []string{fmt.Sprintf("got %v, want %v", got, want)}
}

func TestCHostileMismatchSetRejectsDrift(t *testing.T) {
	t.Parallel()
	want := []string{"known"}
	exact := []string{cHostileFindingPrefix + "known got refuse want accept"}
	if drift := cHostileMismatchSetDrift(exact, want); len(drift) != 0 {
		t.Fatalf("exact carried mismatch set was rejected: %v", drift)
	}
	for label, changed := range map[string][]string{
		"newly passing expected row": nil,
		"new unexpected failure":     {cHostileFindingPrefix + "new got refuse want accept", exact[0]},
	} {
		t.Run(label, func(t *testing.T) {
			if drift := cHostileMismatchSetDrift(changed, want); len(drift) == 0 {
				t.Fatalf("mismatch set drift was accepted: %v", changed)
			}
		})
	}
}

func loadCHostileManifest(t *testing.T, corpusDir string) []cHostileCase {
	t.Helper()
	f, err := os.Open(filepath.Join(corpusDir, "manifest.tsv"))
	if err != nil {
		t.Fatalf("the shared hostile corpus is not committed at %s: %v (the corpus card owns it; this port only reads it)", corpusDir, err)
	}
	defer f.Close()
	var cases []cHostileCase
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := sc.Text()
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		f := strings.Split(s, "\t")
		if len(f) != 9 {
			t.Fatalf("manifest line %d: %d fields, want 9", line, len(f))
		}
		cases = append(cases, cHostileCase{
			name: f[0], carrier: f[1], schema: f[2], table: f[3], input: f[4],
			verdict: f[6], reason: f[7], digest: f[8],
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the hostile manifest names no cases")
	}
	return cases
}

func cHostilePayload(t *testing.T, corpusDir, input string) []byte {
	t.Helper()
	rel := strings.TrimPrefix(input, "inputs/")
	if rel == input {
		t.Fatalf("input %q is not under inputs/", input)
	}
	b, err := os.ReadFile(filepath.Join(corpusDir, "inputs", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("input %q: %v", input, err)
	}
	return b
}

func cSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := rune(s[i-1])
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// generateCHostileUnit emits one schema's C table source into a fresh
// directory and returns the files by name.
func generateCHostileUnit(t *testing.T, root, schema string) map[string][]byte {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == "schemas/probe.schema" {
		dir := t.TempDir()
		path = filepath.Join(dir, "probe.schema")
		if err := os.WriteFile(path, []byte(hostileProbeSchema), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		t.Fatalf("schema %s: %v", schema, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("schema %s does not load: %v", schema, err)
	}
	files, err := codegenc.Generate(u)
	if err != nil {
		t.Fatalf("schema %s: c: %v", schema, err)
	}
	tables, err := ctable.Generate(u)
	if err != nil {
		t.Fatalf("schema %s: ctable: %v", schema, err)
	}
	maps.Copy(files, tables)
	return files
}

// headerFor finds the generated header that declares the table's storage.
func headerFor(files map[string][]byte, table string) string {
	needle := []byte("typedef struct " + table + " {")
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") && strings.Contains(string(data), string(needle)) {
			return name
		}
	}
	return ""
}

// cHostileProbe is the C program behind the runner: one function per case,
// because the table's storage type and codec names are compile-time.
func cHostileProbe(header, headerText string, cases []cHostileCase) string {
	var b strings.Builder
	b.WriteString("#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\n#include <stdint.h>\n")
	fmt.Fprintf(&b, "#include \"%s\"\n\n", header)
	b.WriteString(`
static unsigned char * read_input( const char * dir, const char * name, int64_t * bytes )
{
    char path[4096]; FILE * f; long n; unsigned char * b;
    snprintf( path, sizeof path, "%s/%s", dir, name );
    f = fopen( path, "rb" );
    if ( f == NULL ) { return NULL; }
    if ( fseek( f, 0, SEEK_END ) != 0 ) { fclose( f ); return NULL; }
    n = ftell( f );
    if ( n < 0 ) { fclose( f ); return NULL; }
    if ( fseek( f, 0, SEEK_SET ) != 0 ) { fclose( f ); return NULL; }
    b = (unsigned char *) malloc( (size_t) n + 1 );
    if ( b == NULL ) { fclose( f ); return NULL; }
    if ( n > 0 && fread( b, 1, (size_t) n, f ) != (size_t) n ) { free( b ); fclose( f ); return NULL; }
    fclose( f );
    *bytes = (int64_t) n;
    return b;
}

static const char * reason_name( int reason )
{
    if ( reason == 1 ) { return "newer_form"; }
    if ( reason == 2 ) { return "message_form_as_file"; }
    if ( reason == 3 ) { return "no_vocabulary"; }
    if ( reason == 4 ) { return "second_announcement"; }
    if ( reason == 5 ) { return "vocabulary_too_large"; }
    if ( reason == 6 ) { return "batch_too_large"; }
    return "damaged";
}

static void refuse( const char * name, const TableReport * report )
{
    printf( "%s\trefuse\t%s\t-\n", name,
            ( report->refused || report->reason != 0 ) ? reason_name( report->reason ) : "damaged" );
}

static void accept( const char * name, const char * json, int64_t bytes )
{
    int64_t i;
    printf( "%s\taccept\t-\t", name );
    for ( i = 0; i < bytes; i++ ) { printf( "%02x", (unsigned char) json[i] ); }
    printf( "\n" );
}

static void error( const char * name, const char * what )
{
    printf( "%s\terror\t%s\t-\n", name, what );
    printf( "PROBE-ERROR %s %s\n", name, what );
}
`)
	for i, c := range cases {
		prefix := cSnake(c.table)
		variable := strings.Contains(headerText, prefix+"_builder_init(")
		file := strings.TrimPrefix(c.input, "inputs/")
		fmt.Fprintf(&b, "\nstatic void case_%d( const char * dir )\n{\n", i)
		fmt.Fprintf(&b, "    int64_t bytes = 0; TableReport report; unsigned char * in;\n")
		fmt.Fprintf(&b, "    in = read_input( dir, \"%s\", &bytes );\n", file)
		fmt.Fprintf(&b, "    if ( in == NULL ) { error( \"%s\", \"no-input\" ); return; }\n", c.name)
		fmt.Fprintf(&b, "    memset( &report, 0, sizeof report );\n")
		switch c.carrier {
		case "text":
			if variable {
				fmt.Fprintf(&b, "    { %sBuilder builder; int ok; const %s * value;\n", c.table, c.table)
				fmt.Fprintf(&b, "      if ( !%s_builder_init( &builder ) ) { error( \"%s\", \"builder\" ); }\n", prefix, c.name)
				fmt.Fprintf(&b, "      else { ok = %s_from_json( &builder, (const char *) in, bytes, &report );\n", prefix)
				fmt.Fprintf(&b, "             if ( !ok || report.malformed ) { refuse( \"%s\", &report ); }\n", c.name)
				fmt.Fprintf(&b, "             else { value = %s_builder_root( &builder ); int64_t need = %s_to_json( value, NULL, 0 ); char * out;\n", prefix, prefix)
				fmt.Fprintf(&b, "                    if ( need < 0 ) { error( \"%s\", \"measure\" ); }\n", c.name)
				fmt.Fprintf(&b, "                    else { out = (char *) malloc( (size_t) need + 1 ); int64_t wrote;\n")
				fmt.Fprintf(&b, "                           if ( out == NULL ) { error( \"%s\", \"oom\" ); }\n", c.name)
				fmt.Fprintf(&b, "                           else { wrote = %s_to_json( value, out, need );\n", prefix)
				fmt.Fprintf(&b, "                                  if ( wrote < 0 ) { error( \"%s\", \"write\" ); }\n", c.name)
				fmt.Fprintf(&b, "                                  else { accept( \"%s\", out, wrote ); } free( out ); } } }\n", c.name)
				fmt.Fprintf(&b, "             %s_builder_shutdown( &builder ); }\n", prefix)
				b.WriteString("    }\n")
			} else {
				fmt.Fprintf(&b, "    { %s value; memset( &value, 0, sizeof value );\n", c.table)
				fmt.Fprintf(&b, "      int ok = %s_from_json( &value, (const char *) in, bytes, &report );\n", prefix)
				fmt.Fprintf(&b, "      if ( !ok || report.malformed ) { refuse( \"%s\", &report ); }\n", c.name)
				fmt.Fprintf(&b, "      else { int64_t need = %s_to_json( &value, NULL, 0 ); char * out;\n", prefix)
				fmt.Fprintf(&b, "             if ( need < 0 ) { error( \"%s\", \"measure\" ); }\n", c.name)
				fmt.Fprintf(&b, "             else { out = (char *) malloc( (size_t) need + 1 ); int64_t wrote;\n")
				fmt.Fprintf(&b, "                    if ( out == NULL ) { error( \"%s\", \"oom\" ); }\n", c.name)
				fmt.Fprintf(&b, "                    else { wrote = %s_to_json( &value, out, need );\n", prefix)
				fmt.Fprintf(&b, "                           if ( wrote < 0 ) { error( \"%s\", \"write\" ); }\n", c.name)
				fmt.Fprintf(&b, "                           else { accept( \"%s\", out, wrote ); } free( out ); } } }\n", c.name)
				b.WriteString("    }\n")
			}
		case "wire":
			// A variable unit's wire read needs a caller storage region; the
			// measure is also the count-refusal gate.
			if variable {
				fmt.Fprintf(&b, "    { TableRefuseReason reason = 0; int64_t need = %s_load_measure_ex( in, bytes, NULL, &reason );\n", prefix)
				fmt.Fprintf(&b, "      if ( need < 0 ) { if ( reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_LENGTH || reason == SCHEMA_TABLE_REFUSE_COUNT_OVER_EXTENT_CAP ) { printf( \"%s\\trefuse\\tcount_refusal\\t-\\n\" ); }\n", c.name)
				fmt.Fprintf(&b, "                       else { refuse( \"%s\", &report ); } }\n", c.name)
				fmt.Fprintf(&b, "      else { unsigned char * region = (unsigned char *) malloc( (size_t) need + 64 );\n")
				fmt.Fprintf(&b, "             if ( region == NULL ) { error( \"%s\", \"oom\" ); }\n", c.name)
				fmt.Fprintf(&b, "             else { const %s * value = %s_load( region, need, in, bytes, &report );\n", c.table, prefix)
				fmt.Fprintf(&b, "                    if ( value == NULL || report.malformed ) { refuse( \"%s\", &report ); }\n", c.name)
				fmt.Fprintf(&b, "                    else { int64_t got = %s_to_json( value, NULL, 0 ); char * out;\n", prefix)
				fmt.Fprintf(&b, "                           if ( got < 0 ) { error( \"%s\", \"measure\" ); }\n", c.name)
				fmt.Fprintf(&b, "                           else { out = (char *) malloc( (size_t) got + 1 ); int64_t wrote;\n")
				fmt.Fprintf(&b, "                                  if ( out == NULL ) { error( \"%s\", \"oom\" ); }\n", c.name)
				fmt.Fprintf(&b, "                                  else { wrote = %s_to_json( value, out, got );\n", prefix)
				fmt.Fprintf(&b, "                                         if ( wrote < 0 ) { error( \"%s\", \"write\" ); }\n", c.name)
				fmt.Fprintf(&b, "                                         else { accept( \"%s\", out, wrote ); } free( out ); } } } free( region ); } }\n", c.name)
				b.WriteString("    }\n")
			} else {
				fmt.Fprintf(&b, "    { %s value; int ok; memset( &value, 0, sizeof value );\n", c.table)
				fmt.Fprintf(&b, "      ok = %s_load( &value, in, bytes, &report );\n", prefix)
				fmt.Fprintf(&b, "      if ( !ok || report.malformed ) { refuse( \"%s\", &report ); }\n", c.name)
				fmt.Fprintf(&b, "      else { int64_t need = %s_to_json( &value, NULL, 0 ); char * out;\n", prefix)
				fmt.Fprintf(&b, "             if ( need < 0 ) { error( \"%s\", \"measure\" ); }\n", c.name)
				fmt.Fprintf(&b, "             else { out = (char *) malloc( (size_t) need + 1 ); int64_t wrote;\n")
				fmt.Fprintf(&b, "                    if ( out == NULL ) { error( \"%s\", \"oom\" ); }\n", c.name)
				fmt.Fprintf(&b, "                    else { wrote = %s_to_json( &value, out, need );\n", prefix)
				fmt.Fprintf(&b, "                           if ( wrote < 0 ) { error( \"%s\", \"write\" ); }\n", c.name)
				fmt.Fprintf(&b, "                           else { accept( \"%s\", out, wrote ); } free( out ); } } }\n", c.name)
				b.WriteString("    }\n")
			}
		case "announce", "announce_second", "announce_bytes", "announce_entries":
			fmt.Fprintf(&b, "    { TableMessageEntry entries[kTableMessageEntriesHere]; TableVocabulary voc = table_vocabulary( entries, kTableMessageEntriesHere ); int ok;\n")
			if c.carrier == "announce_bytes" {
				b.WriteString("      voc.max_bytes = 8;\n")
			}
			if c.carrier == "announce_entries" {
				b.WriteString("      voc.max_entries = 1;\n")
			}
			fmt.Fprintf(&b, "      ok = announce_read( &voc, in, bytes, &report );\n")
			if c.carrier == "announce_second" {
				fmt.Fprintf(&b, "      if ( !ok ) { refuse( \"%s\", &report ); }\n", c.name)
				fmt.Fprintf(&b, "      else { TableReport second; memset( &second, 0, sizeof second ); if ( announce_read( &voc, in, bytes, &second ) ) { printf( \"%s\\taccept\\t-\\t-\\n\" ); } else { refuse( \"%s\", &second ); } }\n", c.name, c.name)
			} else {
				fmt.Fprintf(&b, "      if ( !ok ) { refuse( \"%s\", &report ); } else { printf( \"%s\\taccept\\t-\\t-\\n\" ); }\n", c.name, c.name)
			}
			b.WriteString("    }\n")
		case "message", "message_novocab":
			fmt.Fprintf(&b, "    { TableMessageEntry entries[kTableMessageEntriesHere]; TableVocabulary voc = table_vocabulary( entries, kTableMessageEntriesHere );\n")
			fmt.Fprintf(&b, "      %s value; int64_t count = 1; int ok; memset( &value, 0, sizeof value );\n", c.table)
			if c.carrier == "message" {
				fmt.Fprintf(&b, "      uint8_t announced[kTableAnnounceBytes]; TableReport announcement; memset( &announcement, 0, sizeof announcement );\n")
				fmt.Fprintf(&b, "      if ( announce( announced, (int64_t) sizeof announced ) < 0 ) { error( \"%s\", \"announce\" ); }\n", c.name)
				fmt.Fprintf(&b, "      if ( !announce_read( &voc, announced, kTableAnnounceBytes, &announcement ) ) { error( \"%s\", \"unit-announcement\" ); }\n", c.name)
			}
			fmt.Fprintf(&b, "      ok = %s_load_messages( &value, &count, &voc, in, bytes, &report );\n", prefix)
			fmt.Fprintf(&b, "      if ( !ok || report.malformed ) { refuse( \"%s\", &report ); }\n", c.name)
			fmt.Fprintf(&b, "      else { int64_t need = %s_to_json( &value, NULL, 0 ); char * out;\n", prefix)
			fmt.Fprintf(&b, "             if ( need < 0 ) { error( \"%s\", \"measure\" ); }\n", c.name)
			fmt.Fprintf(&b, "             else { out = (char *) malloc( (size_t) need + 1 ); int64_t wrote;\n")
			fmt.Fprintf(&b, "                    if ( out == NULL ) { error( \"%s\", \"oom\" ); }\n", c.name)
			fmt.Fprintf(&b, "                    else { wrote = %s_to_json( &value, out, need );\n", prefix)
			fmt.Fprintf(&b, "                           if ( wrote < 0 ) { error( \"%s\", \"write\" ); }\n", c.name)
			fmt.Fprintf(&b, "                           else { accept( \"%s\", out, wrote ); } free( out ); } } }\n", c.name)
			b.WriteString("    }\n")
		default:
			t := fmt.Sprintf("%q", c.carrier)
			fmt.Fprintf(&b, "    error( \"%s\", \"unknown-carrier-%s\" );\n", c.name, t)
		}
		fmt.Fprintf(&b, "    free( in );\n}\n")
	}
	fmt.Fprintf(&b, "\nint main( int argc, char ** argv )\n{\n    if ( argc < 2 ) { return 2; }\n")
	for i := range cases {
		fmt.Fprintf(&b, "    case_%d( argv[1] );\n", i)
	}
	b.WriteString("    return 0;\n}\n")
	return b.String()
}

// runCHostileCorpus runs the corpus and returns one FIX-CARD line per mismatch.
func runCHostileCorpus(t *testing.T, root, corpusDir string, plant bool) []string {
	t.Helper()
	cases := loadCHostileManifest(t, corpusDir)
	type groupKey struct{ schema, header string }
	groups := map[groupKey][]cHostileCase{}
	generated := map[string]map[string][]byte{}
	for _, c := range cases {
		files, ok := generated[c.schema]
		if !ok {
			files = generateCHostileUnit(t, root, c.schema)
			generated[c.schema] = files
		}
		header := headerFor(files, c.table)
		if header == "" {
			t.Fatalf("schema %s: no generated header declares table %s", c.schema, c.table)
		}
		k := groupKey{c.schema, header}
		groups[k] = append(groups[k], c)
	}
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].schema != keys[j].schema {
			return keys[i].schema < keys[j].schema
		}
		return keys[i].header < keys[j].header
	})
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	var findings []string
	for _, key := range keys {
		group := groups[key]
		files := generated[key.schema]
		dir := t.TempDir()
		for name, data := range files {
			if plant && strings.HasSuffix(name, "Table.c") {
				// A READER BUG, switched on only by the negative control: the
				// generated text reader refuses every JSON input.
				needle := "static SCHEMA_UNUSED int table_json_read( void * value, const TableTypeInfo * info, const char * text, int64_t bytes, TableReport * report )\n{"
				bad := strings.Replace(string(data), needle, needle+"\n    report->malformed = 1; return 0;", 1)
				if bad == string(data) {
					t.Fatalf("the plant did not land in %s", name)
				}
				data = []byte(bad)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		source := cHostileProbe(key.header, string(files[key.header]), group)
		if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		// One generated unit may span files whose descriptors cross-reference
		// each other (tables/pointers' Graph.schema names types from Peers),
		// so every table source of the unit joins the link.
		serialize := os.Getenv("SERIALIZE_C")
		if serialize == "" {
			serialize = filepath.Join(root, "..", "serialize.c")
		}
		args := []string{"-std=c99", "-O0", "-ffp-contract=off", "-I", dir, "-I", serialize, filepath.Join(dir, "probe.c")}
		var sources []string
		for name := range files {
			if strings.HasSuffix(name, "Table.c") {
				sources = append(sources, name)
			}
		}
		sort.Strings(sources)
		for _, name := range sources {
			args = append(args, filepath.Join(dir, name))
		}
		binary := filepath.Join(dir, "probe")
		args = append(args, "-lm", "-o", binary)
		cmd := exec.Command(cc, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s/%s: the generated reader does not compile: %v\n%s", key.schema, key.header, err, out)
		}
		run := exec.Command(binary, filepath.Join(corpusDir, "inputs"))
		out, err := run.CombinedOutput()
		if err != nil {
			t.Fatalf("%s/%s: probe failed: %v\n%s", key.schema, key.header, err, out)
		}
		got := map[string][4]string{}
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				continue
			}
			got[f[0]] = [4]string{f[0], f[1], f[2], f[3]}
		}
		for _, c := range group {
			g, ok := got[c.name]
			if !ok {
				findings = append(findings, fmt.Sprintf("FIX-CARD: c-hostile-corpus: %s the runner printed no verdict", c.name))
				continue
			}
			digest := "-"
			if c.verdict == "accept" && g[1] == "accept" {
				canonical, err := hex.DecodeString(g[3])
				if err != nil {
					digest = "not-hex:" + g[3]
				} else {
					// The C text writer closes its document with a newline; the
					// engine's canonical text does not. That byte is formatting,
					// not the decoded value, so the digest is over the document.
					canonical = bytes.TrimRight(canonical, "\n")
					sum := sha256.Sum256(canonical)
					digest = hex.EncodeToString(sum[:])
				}
			}
			if g[1] != c.verdict || g[2] != c.reason || (c.verdict == "accept" && digest != c.digest) {
				findings = append(findings, fmt.Sprintf(
					"FIX-CARD: c-hostile-corpus: %s got %s/%s/%s want %s/%s/%s",
					c.name, g[1], g[2], digest, c.verdict, c.reason, c.digest))
			}
		}
	}
	return findings
}

// TestCRunsHostileCorpus runs every manifest input through the generated C
// reader at the manifest's carrier and holds it to the committed verdict.
func TestCRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	findings := runCHostileCorpus(t, root, filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"), false)
	for _, drift := range cHostileMismatchSetDrift(findings, cExpectedHostileMismatches) {
		t.Errorf("hostile mismatch list drift: %s", drift)
	}
	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) > 0 {
		t.Fatalf("%d hostile-corpus mismatches", len(findings))
	}
}

// TestCRunsHostileCorpusPlantedPattern is the control: the same runner, with a
// reader bug planted in the scratch copy only, must go red and name the inputs
// it refuses.
func TestCRunsHostileCorpusPlantedPattern(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	findings := runCHostileCorpus(t, root, filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus"), true)
	if len(findings) == 0 {
		t.Fatal("the planted reader bug left the hostile-corpus runner green")
	}
	found := 0
	for _, f := range findings {
		if strings.Contains(f, "FIX-CARD: c-hostile-corpus:") {
			found++
		}
	}
	if found == 0 {
		t.Fatal("the planted reader bug was caught without a FIX-CARD line")
	}
}
