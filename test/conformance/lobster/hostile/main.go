// hostile is THE LOBSTER PORT's run of the shared hostile corpus
// (docs/HOSTILE-CORPUS.md; schema-lobster-runs-hostile-corpus in
// make/lobster-hostile.mk).
//
// For every schema the manifest names it generates the Lobster unit, stages
// test/conformance/lobster/hostile.lobster in it, runs it once under the
// pinned toolchain, and holds every answer to the committed engine verdict.
// A table accept is held by the SHA-256 of the decoded value's canonical
// text: the port's own JSON writer, whose one shape difference from the
// engine's (internal/tabletext.Model.Write) is the trailing newline, dropped
// here. A schema of packet types has no table unit: its rows are read from
// bit 0 through the generated Read<Root> as packet_refused, and an accept is
// held by the SHA-256 of the bytes Write<Root> writes back.
//
// A disagreement the port already owes is carried by name in the findings
// file and printed as a FIX-CARD; one no finding names fails, and a finding
// whose case has gone green fails too, so the list cannot rot. Reads files
// only: no network, no clock.
//
//	hostile [--corpus dir] [--units dir] [--findings file] [--no-generate]
//
// --no-generate runs over units already in --units (the negative control
// plants a reader defect in a copy and runs that). The working directory is
// the repository root.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
)

const program = "test/conformance/lobster/hostile.lobster"

// packetMarker divides the staged program: everything before it is the table
// program, everything from it on is the packet program. A schema of packet
// types has no table unit to import, so it is answered by its generated
// Read<Root>/Write<Root> instead.
const packetMarker = "// ==== PACKET UNIT ===="

var (
	packetReadRe  = regexp.MustCompile(`(?m)^def Read([A-Za-z_][A-Za-z0-9_]*)\(stream:serialize_read_stream, value:[A-Za-z_][A-Za-z0-9_]*\) -> bool:`)
	packetWriteRe = regexp.MustCompile(`(?m)^def Write([A-Za-z_][A-Za-z0-9_]*)\(s:serialize_write_stream, v:[A-Za-z_][A-Za-z0-9_]*\) -> bool:`)
	packetNewRe   = regexp.MustCompile(`(?m)^def New([A-Za-z_][A-Za-z0-9_]*)\(\) -> [A-Za-z_][A-Za-z0-9_]*:`)
	namespaceRe   = regexp.MustCompile(`(?m)^namespace ([A-Za-z_][A-Za-z0-9_]*)`)
)

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "lobster hostile corpus: "+format+"\n", args...)
	os.Exit(1)
}

// unitDir is the directory one schema's unit is generated into.
func unitDir(units, schema string) string {
	return filepath.Join(units, strings.NewReplacer("/", "_", ".", "_").Replace(schema))
}

// schemaPath is the schema on disk: the checkout's own, or the probe schema
// the corpus package carries, written beside the units.
func schemaPath(units, schema string) (string, error) {
	if schema != hostilecorpus.ProbeSchemaPath {
		return filepath.FromSlash(schema), nil
	}
	path := filepath.Join(units, "probe.schema")
	return path, os.WriteFile(path, []byte(hostilecorpus.ProbeSchema), 0o644)
}

// refusal is the Lobster target refusing a schema the corpus names: every case
// of that schema is a NAMED ABSENCE, counted and printed with the refusal.
type refusal struct{ err error }

func (r refusal) Error() string { return r.err.Error() }

// firstLine is a refusal's first line, which names the construct.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func generate(units, schema string) error {
	path, err := schemaPath(units, schema)
	if err != nil {
		return err
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return err
	}
	c := compiler.New()
	u, err := c.Load(paths)
	if err != nil {
		return err
	}
	files, err := c.Generate(u, "lobster", compiler.Options{})
	if err != nil {
		return refusal{fmt.Errorf("lobster refuses %s: %w", schema, err)}
	}
	dir := unitDir(units, schema)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// packetUnit is a schema whose every row is a packet case: it generates a
// packet unit, not a table one.
func packetUnit(cases []hostilecorpus.Case, schema string) bool {
	found := false
	for _, c := range cases {
		if c.Schema != schema {
			continue
		}
		if c.Carrier != "packet" {
			return false
		}
		found = true
	}
	return found
}

// programText is the staged program: the table part before the marker, or the
// packet part from the marker on.
func programText(packet bool) (string, error) {
	data, err := os.ReadFile(program)
	if err != nil {
		return "", err
	}
	text := string(data)
	at := strings.Index(text, packetMarker)
	if at < 0 {
		if packet {
			return "", fmt.Errorf("%s: no %q marker", program, packetMarker)
		}
		return text, nil
	}
	if packet {
		return text[at:], nil
	}
	return text[:at], nil
}

// packetModule finds the generated packet module in the unit: its import base,
// its namespace, and the packet types it emits a New, Read and Write for.
func packetModule(dir string) (base, namespace string, types []string, err error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.lobster"))
	if err != nil {
		return "", "", nil, err
	}
	sort.Strings(paths)
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasSuffix(name, "View.lobster") || strings.HasPrefix(name, "serialize") ||
			name == "descriptors.lobster" || strings.HasPrefix(name, "schema_") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", nil, err
		}
		reads := packetReadRe.FindAllSubmatch(data, -1)
		if len(reads) == 0 {
			continue
		}
		ns := namespaceRe.FindSubmatch(data)
		if ns == nil {
			return "", "", nil, fmt.Errorf("%s: no namespace", path)
		}
		base = strings.TrimSuffix(name, ".lobster")
		namespace = string(ns[1])
		writes := map[string]bool{}
		for _, m := range packetWriteRe.FindAllSubmatch(data, -1) {
			writes[string(m[1])] = true
		}
		news := map[string]bool{}
		for _, m := range packetNewRe.FindAllSubmatch(data, -1) {
			news[string(m[1])] = true
		}
		seen := map[string]bool{}
		for _, m := range reads {
			t := string(m[1])
			if seen[t] || !writes[t] || !news[t] {
				continue
			}
			seen[t] = true
			types = append(types, t)
		}
		return base, namespace, types, nil
	}
	return "", "", nil, fmt.Errorf("%s: no generated packet reader", dir)
}

// packetDispatch generates the type switch the packet program calls for each
// row: read from bit 0 through Read<Root>, require a clean end, and write the
// value back through Write<Root> for the accept digest.
func packetDispatch(namespace string, types []string) string {
	var b strings.Builder
	b.WriteString("// hexs spells bytes as lowercase hex, the answer line's accept payload.\n")
	b.WriteString("def hexs(b:string) -> string:\n")
	b.WriteString("    var s = \"\"\n")
	b.WriteString("    for(b) c:\n")
	b.WriteString("        s += number_to_string(c, 16, 2)\n")
	b.WriteString("    return lowercase(s)\n\n")
	b.WriteString("// packet_capacity is a write buffer large enough for the value's wire.\n")
	b.WriteString("def packet_capacity(data:string) -> int:\n")
	b.WriteString("    return ((length(data) * 2 + 4096 + 7) / 8) * 8\n\n")
	b.WriteString("// packet_answer reads one packet row through the unit's generated reader and\n")
	b.WriteString("// writes an accepted value back through its generated writer.\n")
	b.WriteString("def packet_answer(name:string, table:string, data:string) -> string:\n")
	for _, t := range types {
		fmt.Fprintf(&b, "    if table == %q:\n", t)
		fmt.Fprintf(&b, "        let v = %s.New%s()\n", namespace, t)
		b.WriteString("        let r = serialize_read_stream_padded(data)\n")
		fmt.Fprintf(&b, "        if not %s.Read%s(r, v):\n", namespace, t)
		b.WriteString("            return name + \" refuse packet_refused\\n\"\n")
		b.WriteString("        if not r.serialize_end():\n")
		b.WriteString("            return name + \" refuse packet_refused\\n\"\n")
		b.WriteString("        let buf = ensure_size(\"\", packet_capacity(data), 0)\n")
		b.WriteString("        let w = serialize_write_stream(buf)\n")
		fmt.Fprintf(&b, "        if not %s.Write%s(w, v):\n", namespace, t)
		b.WriteString("            return name + \" unsaved packet_writer_refused\\n\"\n")
		b.WriteString("        w.flush()\n")
		b.WriteString("        return name + \" accept \" + hexs(substring(buf, 0, w.bytes_processed())) + \"\\n\"\n")
	}
	b.WriteString("    return name + \" error no generated packet reader for \" + table + \"\\n\"\n")
	return b.String()
}

// run stages the program in the unit and answers every case of the schema.
func run(units, corpus, schema string, packet bool) (map[string][]string, error) {
	dir := unitDir(units, schema)
	src, err := programText(packet)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	if packet {
		base, namespace, types, err := packetModule(dir)
		if err != nil {
			return nil, err
		}
		b.WriteString("// GENERATED by test/conformance/lobster/hostile from hostile.lobster: the unit's packet module, then the packet program\n")
		fmt.Fprintf(&b, "import .%s\n", base)
		b.WriteString("import .serialize\nimport .serialize_stream\n")
		b.WriteString(packetDispatch(namespace, types))
		b.WriteString(src)
	} else {
		tables, err := filepath.Glob(filepath.Join(dir, "*Table.lobster"))
		if err != nil {
			return nil, err
		}
		sort.Strings(tables)
		b.WriteString("// GENERATED by test/conformance/lobster/hostile from hostile.lobster: the unit's table modules, then the program\n")
		for _, t := range tables {
			fmt.Fprintf(&b, "import .%s\n", strings.TrimSuffix(filepath.Base(t), ".lobster"))
		}
		b.WriteString(src)
	}
	main := filepath.Join(dir, "hostile_main.lobster")
	if err := os.WriteFile(main, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	lobster := os.Getenv("LOBSTER")
	if lobster == "" {
		lobster = "dist/lobster-v2026.8/bin/lobster"
	}
	abs := func(p string) string {
		a, err := filepath.Abs(p)
		if err != nil {
			fatalf("%v", err)
		}
		return a
	}
	answers := filepath.Join(dir, "hostile_answers.txt")
	_ = os.Remove(answers)
	cmd := exec.Command(abs(lobster), abs(main), "--", abs(corpus), schema, abs(answers))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", schema, err, out)
	}
	if len(out) != 0 {
		// a compiler warning is printed on stdout: generated Lobster compiles
		// with no warning, and the program prints nothing else
		return nil, fmt.Errorf("%s: the program printed (a warning fails the run):\n%s", schema, out)
	}
	data, err := os.ReadFile(answers)
	if err != nil {
		return nil, err
	}
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("%s: not an answer line: %q", schema, line)
		}
		got[f[0]] = f[1:]
	}
	return got, nil
}

// digest is the answer's SHA-256. A table accept hashes the canonical text:
// the port's JSON text with its one shape difference from the engine's
// removed, the trailing newline. A packet accept hashes the bytes the port's
// writer wrote back.
func digest(answerHex string, packet bool) (string, error) {
	if answerHex == "-" {
		return "-", nil
	}
	value, err := hex.DecodeString(answerHex)
	if err != nil {
		return "", err
	}
	if packet {
		sum := sha256.Sum256(value)
		return hex.EncodeToString(sum[:]), nil
	}
	sum := sha256.Sum256(bytes.TrimSuffix(value, []byte("\n")))
	return hex.EncodeToString(sum[:]), nil
}

// readFindings: name<TAB>one line, `#` comments.
func readFindings(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, text, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("%s: %q is not name<TAB>finding", path, line)
		}
		out[name] = text
	}
	return out, nil
}

func main() {
	corpus := flag.String("corpus", "internal/ci/hostilecorpus/corpus", "the committed corpus directory")
	units := flag.String("units", "build/lobster-hostile", "where each schema's unit is generated")
	findingsPath := flag.String("findings", "test/conformance/lobster/hostile-known-findings.tsv", "the carried disagreements")
	noGenerate := flag.Bool("no-generate", false, "run over the units already in --units")
	flag.Parse()

	_, cases, err := hostilecorpus.LoadManifest(*corpus)
	if err != nil {
		fatalf("%v", err)
	}
	findings, err := readFindings(*findingsPath)
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.MkdirAll(*units, 0o755); err != nil {
		fatalf("%v", err)
	}
	var schemas []string
	seen := map[string]bool{}
	for _, c := range cases {
		if !seen[c.Schema] {
			seen[c.Schema] = true
			schemas = append(schemas, c.Schema)
		}
	}
	sort.Strings(schemas)
	packet := map[string]bool{}
	for _, schema := range schemas {
		packet[schema] = packetUnit(cases, schema)
	}
	answers := map[string]map[string][]string{}
	absent := map[string]string{}
	for _, schema := range schemas {
		if !*noGenerate {
			if err := generate(*units, schema); err != nil {
				if r, ok := errors.AsType[refusal](err); ok {
					absent[schema] = firstLine(r.Error())
					continue
				}
				fatalf("%v", err)
			}
		} else if _, err := os.Stat(unitDir(*units, schema)); os.IsNotExist(err) {
			absent[schema] = "no unit generated for " + schema
			continue
		}
		if answers[schema], err = run(*units, *corpus, schema, packet[schema]); err != nil {
			fatalf("%v", err)
		}
	}

	pass, carried, missing := 0, 0, 0
	var bad, stale []string
	for _, c := range cases {
		if why, no := absent[c.Schema]; no {
			missing++
			fmt.Printf("ABSENT %s carrier=%s: %s\n", c.Name, c.Carrier, why)
			if finding, owed := findings[c.Name]; owed {
				stale = append(stale, fmt.Sprintf("STALE FIX-CARD %s: the case is absent; delete its line (%s)", c.Name, finding))
			}
			continue
		}
		a, ok := answers[c.Schema][c.Name]
		var got string
		switch {
		case !ok:
			got = "no answer"
		case a[0] == "accept":
			d, err := digest(a[1], c.Carrier == "packet")
			if err != nil {
				got = "accept, " + err.Error()
			} else {
				got = "accept " + d
			}
		case a[0] == "refuse":
			got = "refuse " + a[1]
		default:
			got = a[0] + " " + a[1]
		}
		want := "refuse " + c.Reason
		if c.Verdict == hostilecorpus.Accept {
			want = "accept " + c.Digest
		}
		finding, owed := findings[c.Name]
		switch {
		case got == want && owed:
			stale = append(stale, fmt.Sprintf("STALE FIX-CARD %s: the case passes; delete its line (%s)", c.Name, finding))
		case got == want:
			pass++
			fmt.Printf("PASS %s\n", c.Name)
		case owed:
			carried++
			fmt.Printf("FIX-CARD %s carrier=%s: %s (got %s, want %s)\n", c.Name, c.Carrier, finding, got, want)
		default:
			bad = append(bad, fmt.Sprintf("FAIL %s carrier=%s got=%s want=%s", c.Name, c.Carrier, got, want))
		}
	}
	for _, line := range bad {
		fmt.Println(line)
	}
	for _, line := range stale {
		fmt.Println(line)
	}
	fmt.Printf("lobster hostile corpus: %d pass, %d carried, %d absent, %d fail, %d stale, of %d cases\n", pass, carried, missing, len(bad), len(stale), len(cases))
	if len(bad) > 0 || len(stale) > 0 {
		os.Exit(1)
	}
}
