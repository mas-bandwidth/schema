package compiler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJsonFixturesHoldNoBareWideInteger holds the shared JSON fixtures to the
// text form's wide-integer rule (docs/SPEC-TABLES.md §16.2; docs/rulings.json
// json-wide-integers-refuse-number-token): an integer field whose kind is wider
// than 32 bits — int64, uint64, int128, uint128, bits(33..64), and a fixed or
// ufixed kind whose storage is 64 or 128 bits — is written as a JSON STRING of
// digits, and a bare number token there refuses. Every fixture a manifest or a
// harness case file binds to a schema is walked against that schema's declared
// field kinds, and any bare number token where the kind is wide fails.
//
// The walk reads the bytes itself: it resolves each value's field from the
// checked unit (ir), never through a reader, so it says the same before and
// after a reader changes. The one exemption is the explicit list
// testdata/wire/json-integers/BARE-NUMBER-PURPOSE.txt: manifests and rows whose
// PURPOSE is the bare token itself.
func TestJsonFixturesHoldNoBareWideInteger(t *testing.T) {
	t.Parallel()
	const root = ".."
	exempt, err := loadWideExemptions(root)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []wideFixture
	for _, g := range []struct {
		source string
		gather func(string) ([]wideFixture, error)
	}{
		{"tables/pack", gatherPackFixtures},
		{"tables/cards", gatherCardFixtures},
		{"testdata/conformance/tables", gatherConformanceFixtures},
		{"testdata/wire/json-integers", gatherJSONIntegerFixtures},
		{"testdata/wire/json-comment-text-rules", gatherCommentRuleFixtures},
		{"internal/ci/hostilecorpus", gatherHostileCorpusFixtures},
		{"testdata/conformance/feature-witnesses", gatherFeatureWitnessFixtures},
	} {
		got, err := g.gather(root)
		if err != nil {
			t.Fatalf("%s: %v", g.source, err)
		}
		// a gatherer that finds nothing has lost its manifest, not cleaned it
		if len(got) == 0 {
			t.Fatalf("%s: no fixture bound; the source moved or its manifest changed shape", g.source)
		}
		fixtures = append(fixtures, got...)
	}

	used := map[string]bool{}
	units := map[string]*ir.Unit{}
	var findings []string
	for _, fx := range fixtures {
		if key, ok := exempt.covers(fx); ok {
			used[key] = true
			continue
		}
		u, ok := units[fx.schema]
		if !ok {
			u, err = loadWideUnit(root, fx.schema)
			if err != nil {
				t.Fatalf("%s: load %s: %v", fx.file, fx.schema, err)
			}
			units[fx.schema] = u
		}
		if fx.anyRoot {
			// a unit that declares no wide value anywhere cannot hold a bare
			// wide integer, whatever root its case file reads
			if f := firstWideValueField(u); f != "" {
				findings = append(findings, fmt.Sprintf("%s: bound to %s, which declares the wide field %s; the checker needs this fixture's root", fx.file, fx.schema, f))
			}
			continue
		}
		st := u.Tables[fx.root]
		if st == nil {
			t.Fatalf("%s: table %s is not in %s", fx.file, fx.root, fx.schema)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(fx.file)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, ok := descendPack(wideCtx{st: st}, fx.at)
		if !ok {
			continue // a hostile pack tree's unknown file names no field
		}
		w := &wideWalk{b: data}
		w.value(ctx, "$")
		for _, h := range w.hits {
			findings = append(findings, fmt.Sprintf("%s: %s %s is a bare number token in a wide integer field (%s); write it as a string", fx.file, h.path, h.token, h.kind))
		}
	}

	textform, err := checkTextformOracles(root)
	if err != nil {
		t.Fatal(err)
	}
	findings = append(findings, textform...)

	for _, key := range exempt.keys {
		if !used[key] {
			findings = append(findings, fmt.Sprintf("BARE-NUMBER-PURPOSE.txt names %q, which binds no fixture: drop the line", key))
		}
	}
	sort.Strings(findings)
	for _, f := range findings {
		t.Error(f)
	}
}

// wideFixture is one JSON file bound to one schema root.
type wideFixture struct {
	schema   string   // repo-relative schema file or directory
	root     string   // the table the text is read at
	file     string   // repo-relative JSON file
	at       []string // a pack tree's path below the root, nil for the whole root
	manifest string   // repo-relative manifest that binds it
	row      string   // the row's name in that manifest
	anyRoot  bool     // bound to a unit only (feature-witness case files)
}

// wideExemptions is BARE-NUMBER-PURPOSE.txt: each line names a manifest, or a
// manifest and one row of it, whose purpose is the bare number token.
type wideExemptions struct {
	keys []string
	set  map[string]bool
}

func loadWideExemptions(root string) (*wideExemptions, error) {
	f, err := os.Open(filepath.Join(root, "testdata/wire/json-integers/BARE-NUMBER-PURPOSE.txt"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ex := &wideExemptions{set: map[string]bool{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		text, reason, ok := strings.Cut(line, "#")
		if !ok || strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("BARE-NUMBER-PURPOSE.txt: %q gives no reason after #", line)
		}
		fields := strings.Fields(text)
		if len(fields) < 1 || len(fields) > 2 {
			return nil, fmt.Errorf("BARE-NUMBER-PURPOSE.txt: %q is not <manifest> [<row>] # <reason>", line)
		}
		key := strings.Join(fields, " ")
		ex.keys = append(ex.keys, key)
		ex.set[key] = true
	}
	return ex, sc.Err()
}

func (ex *wideExemptions) covers(fx wideFixture) (string, bool) {
	if ex.set[fx.manifest] {
		return fx.manifest, true
	}
	key := fx.manifest + " " + fx.row
	return key, ex.set[key]
}

func loadWideUnit(root, schema string) (*ir.Unit, error) {
	paths, err := GatherPaths([]string{filepath.Join(root, filepath.FromSlash(schema))})
	if err != nil {
		return nil, err
	}
	return New().Load(paths)
}

// wideKind names a field's kind when it is a wide integer — the kinds §16.2
// writes as a string of digits — and "" otherwise. A timestamp is an int64
// count with its own text (an RFC 3339 string), so it is not one.
func wideKind(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TInt:
		if t.Width > 32 && !t.Timestamp {
			if t.Signed {
				return fmt.Sprintf("int%d", t.Width)
			}
			return fmt.Sprintf("uint%d", t.Width)
		}
	case ir.TBits:
		if t.Width > 32 {
			return fmt.Sprintf("bits(%d)", t.Width)
		}
	case ir.TFixed:
		if t.Width > 32 {
			if t.Signed {
				return fmt.Sprintf("fixed(%d, %d)", t.IntBits, t.FracBits)
			}
			return fmt.Sprintf("ufixed(%d, %d)", t.IntBits, t.FracBits)
		}
	}
	return ""
}

// firstWideValueField names a wide field anywhere in the unit's tables and
// types, a map's KEY aside (a key is always a JSON string), or "".
func firstWideValueField(u *ir.Unit) string {
	var names []string
	for _, st := range u.Tables {
		for _, f := range st.Fields {
			if wideKind(f) != "" && !(st.IsMapEntry() && f.Name == ir.MapKeyFieldName) {
				names = append(names, st.Name+"."+f.Name)
			}
		}
	}
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if wideKind(f) != "" {
				names = append(names, st.Name+"."+f.Name)
			}
		}
	}
	for _, un := range u.TableUnions {
		for _, v := range un.Variants {
			if v.F != nil && wideKind(v.F) != "" {
				names = append(names, un.Name+"."+v.Name)
			}
		}
	}
	if len(names) == 0 {
		return ""
	}
	slices.Sort(names)
	return names[0]
}

// wideCtx is where a JSON value sits in the schema: a table or type (st), a
// union (un), or a field (f) — elem set once the field's array or map shape is
// consumed, so the value is one element. The zero value is a value nothing
// declares (an unknown key, a label), walked for its shape and never judged.
type wideCtx struct {
	st   *ir.Struct
	un   *ir.Union
	f    *ir.Field
	elem bool
}

// collection reports whether a field's text is an array or object of
// elements: an array, an enum-keyed array or a map.
func collection(f *ir.Field) bool {
	return f.Array != ir.ArrayNone || f.KeyEnum != "" || f.Type.Kind == ir.TMap
}

// element is the context of one element of a collection field.
func element(f *ir.Field) wideCtx {
	if f.Type.Kind == ir.TMap {
		return fieldCtx(ir.MapValueField(f))
	}
	return named(f, true)
}

func fieldCtx(f *ir.Field) wideCtx {
	if collection(f) {
		return wideCtx{f: f}
	}
	return named(f, true)
}

// named resolves a single value of field f: a nested table or type, a union,
// or the scalar field itself.
func named(f *ir.Field, elem bool) wideCtx {
	if f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			if ref.TextElem {
				return wideCtx{}
			}
			return wideCtx{st: ref}
		case *ir.Union:
			return wideCtx{un: ref}
		}
	}
	return wideCtx{f: f, elem: elem}
}

func (c wideCtx) key(k string) wideCtx {
	switch {
	case c.st != nil:
		if strings.HasPrefix(k, "&") {
			return wideCtx{}
		}
		for _, f := range c.st.Fields {
			name := f.Name
			if f.JsonKey != "" {
				name = f.JsonKey
			}
			if name == k {
				return fieldCtx(f)
			}
		}
	case c.un != nil:
		for _, v := range c.un.Variants {
			if v.Name != k {
				continue
			}
			if v.F != nil {
				return fieldCtx(v.F)
			}
			if v.Ref != nil {
				return wideCtx{st: v.Ref}
			}
		}
	case c.f != nil && !c.elem && collection(c.f):
		// an enum-keyed array or a map: every key is one element
		if c.f.KeyEnum != "" || c.f.Type.Kind == ir.TMap {
			return element(c.f)
		}
	}
	return wideCtx{}
}

func (c wideCtx) index() wideCtx {
	switch {
	case c.st != nil && c.st.JsonArray && len(c.st.Fields) == 1:
		return element(c.st.Fields[0])
	case c.f != nil && !c.elem && c.f.Array != ir.ArrayNone && c.f.KeyEnum == "":
		return element(c.f)
	}
	return wideCtx{}
}

// descendPack resolves a pack tree's file path below the root (§17): each
// directory or file name is a field of the table it sits in, or one element
// of the collection field above it.
func descendPack(c wideCtx, at []string) (wideCtx, bool) {
	for _, seg := range at {
		switch {
		case c.st != nil:
			next := c.key(seg)
			if next == (wideCtx{}) {
				return c, false
			}
			c = next
		case c.f != nil && !c.elem && collection(c.f):
			c = element(c.f)
		default:
			return c, false
		}
	}
	return c, true
}

type wideHit struct{ path, token, kind string }

// wideWalk is a lenient JSON walk: it accepts what the hostile vectors hold
// (comments, a BOM, trailing commas, repeated keys, broken tails) and judges
// every number token it meets against the field the value sits in.
type wideWalk struct {
	b    []byte
	i    int
	hits []wideHit
}

func (w *wideWalk) space() {
	for w.i < len(w.b) {
		c := w.b[w.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' || c == ':':
			w.i++
		case c == 0xEF && w.i+2 < len(w.b) && w.b[w.i+1] == 0xBB && w.b[w.i+2] == 0xBF:
			w.i += 3
		case c == '/' && w.i+1 < len(w.b) && w.b[w.i+1] == '/':
			for w.i < len(w.b) && w.b[w.i] != '\n' {
				w.i++
			}
		case c == '/' && w.i+1 < len(w.b) && w.b[w.i+1] == '*':
			end := strings.Index(string(w.b[w.i+2:]), "*/")
			if end < 0 {
				w.i = len(w.b)
				return
			}
			w.i += end + 4
		default:
			return
		}
	}
}

func (w *wideWalk) str() string {
	start := w.i + 1
	w.i++
	for w.i < len(w.b) {
		switch w.b[w.i] {
		case '\\':
			w.i += 2
			continue
		case '"':
			s := string(w.b[start:w.i])
			w.i++
			return s
		}
		w.i++
	}
	return string(w.b[start:])
}

func numberByte(c byte) bool {
	return c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' || (c >= '0' && c <= '9')
}

func (w *wideWalk) value(c wideCtx, path string) {
	w.space()
	if w.i >= len(w.b) {
		return
	}
	switch ch := w.b[w.i]; {
	case ch == '{':
		w.i++
		for {
			w.space()
			if w.i >= len(w.b) {
				return
			}
			if w.b[w.i] == '}' {
				w.i++
				return
			}
			if w.b[w.i] != '"' {
				w.i++ // not a key: step over it
				continue
			}
			k := w.str()
			w.space()
			w.value(c.key(k), path+"."+k)
		}
	case ch == '[':
		w.i++
		for n := 0; ; n++ {
			w.space()
			if w.i >= len(w.b) {
				return
			}
			if w.b[w.i] == ']' {
				w.i++
				return
			}
			w.value(c.index(), fmt.Sprintf("%s[%d]", path, n))
		}
	case ch == '"':
		w.str()
	case numberByte(ch):
		start := w.i
		for w.i < len(w.b) && numberByte(w.b[w.i]) {
			w.i++
		}
		if c.f != nil && c.elem {
			if kind := wideKind(c.f); kind != "" {
				w.hits = append(w.hits, wideHit{path, string(w.b[start:w.i]), kind})
			}
		}
	default:
		// a literal (true, false, null) or a stray byte
		start := w.i
		for w.i < len(w.b) && w.b[w.i] >= 'a' && w.b[w.i] <= 'z' {
			w.i++
		}
		if w.i == start {
			w.i++
		}
	}
}

// jsonFiles lists the .json files under dir, repo-relative, in order.
func jsonFiles(root, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".json") {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out, err
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func manifestLines(root, rel string) ([][]string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	return splitManifest(data), nil
}

// splitManifest splits manifest text into the fields of its rows.
func splitManifest(data []byte) [][]string {
	var out [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, strings.Fields(line))
	}
	return out
}

// gatherPackFixtures binds the pack corpus (§17): each tree is read at its
// root, and a file's path below the tree names the field it holds.
func gatherPackFixtures(root string) ([]wideFixture, error) {
	trees := []struct{ dir, table string }{
		{"tables/pack/config", "PackConfig"},
		{"tables/pack/root", "RootConfig"},
		{"tables/pack/pinned/PackConfig", "PackConfig"},
		{"tables/pack/pinned/RootConfig", "RootConfig"},
	}
	cases, err := os.ReadDir(filepath.Join(root, "tables/pack/hostile"))
	if err != nil {
		return nil, err
	}
	for _, c := range cases {
		if c.IsDir() {
			trees = append(trees, struct{ dir, table string }{"tables/pack/hostile/" + c.Name(), "PackConfig"})
		}
	}
	var out []wideFixture
	for _, tree := range trees {
		files, err := jsonFiles(root, tree.dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			at := strings.Split(strings.TrimSuffix(strings.TrimPrefix(file, tree.dir+"/"), ".json"), "/")
			if len(at) == 1 && at[0] == tree.table {
				at = nil // the whole root in one file
			}
			out = append(out, wideFixture{schema: "tables/examples", root: tree.table, file: file, at: at, manifest: tree.dir, row: file})
		}
	}
	return out, nil
}

// gatherCardFixtures binds the card conformance cases and answers, the card
// fuzz seeds, and the card corpus, each corpus case at its own frozen
// declaration.
func gatherCardFixtures(root string) ([]wideFixture, error) {
	var out []wideFixture
	for _, dir := range []string{"tables/cards/conformance", "tables/cards/fuzz"} {
		files, err := jsonFiles(root, dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			out = append(out, wideFixture{schema: "tables/cards/Card.schema", root: "Card", file: file, manifest: dir + "/MANIFEST.txt", row: file})
		}
	}
	dirs, err := os.ReadDir(filepath.Join(root, "tables/cards/corpus"))
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := "tables/cards/corpus/" + d.Name()
		files, err := jsonFiles(root, dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			out = append(out, wideFixture{schema: dir + "/DECLARATION.schema", root: "Card", file: file, manifest: dir, row: file})
		}
	}
	return out, nil
}

// gatherConformanceFixtures binds every JSON file a row of the tables
// conformance manifests names: a json-hostile tree and its value golden, an
// instance's text, a report's and a retain's value, a rewrite's text, a framing
// row's value and a cook-write row's input. A JSON file under the directory
// that no row binds fails, so a fixture cannot sit outside the walk.
func gatherConformanceFixtures(root string) ([]wideFixture, error) {
	const dir = "testdata/conformance/tables"
	unitPath := map[string]string{}
	type bound struct{ unit, table string }
	instances := map[string]bound{}
	var out []wideFixture
	bind := func(manifest, row, unit, table, file string) error {
		schema, ok := unitPath[unit]
		if !ok {
			return fmt.Errorf("%s row %s names unit %s, which no unit line declares", manifest, row, unit)
		}
		out = append(out, wideFixture{schema: schema, root: table, file: file, manifest: manifest, row: row})
		return nil
	}
	for _, manifest := range []string{dir + "/MANIFEST.txt", dir + "/JSON-HOSTILE-NUMBER-TOKEN.txt"} {
		rows, err := manifestLines(root, manifest)
		if err != nil {
			// an absent number-token manifest binds nothing; its exemption
			// line then fails as one that binds no fixture
			if os.IsNotExist(err) && manifest != dir+"/MANIFEST.txt" {
				continue
			}
			return nil, err
		}
		for _, f := range rows {
			if f[0] == "unit" && len(f) >= 3 {
				unitPath[f[1]] = f[2]
			}
			if f[0] == "instance" && len(f) >= 4 {
				instances[f[1]] = bound{f[2], f[3]}
			}
		}
		for _, f := range rows {
			if len(f) < 4 {
				continue
			}
			name, unit, table := f[1], f[2], f[3]
			var files []string
			switch f[0] {
			case "json-hostile":
				files = append(files, f[4]+"/"+table+".json")
				if v := f[4] + ".value.json"; exists(root, v) {
					files = append(files, v)
				}
			case "instance":
				files = append(files, dir+"/json/"+name+".json")
			case "report":
				files = append(files, dir+"/report-values/"+name+".json")
			case "retain":
				files = append(files, dir+"/retain-values/"+name+".json")
			case "rewrite":
				files = append(files, dir+"/rewrite/"+name+".json")
			case "framing":
				if len(f) >= 7 && f[6] != "-" {
					files = append(files, f[6])
				}
			case "cook-write":
				for _, opt := range f[4:] {
					if input, ok := strings.CutPrefix(opt, "input="); ok {
						b, ok := instances[name]
						if !ok {
							return nil, fmt.Errorf("%s: cook-write %s names no instance", manifest, name)
						}
						unit, table = b.unit, b.table
						files = append(files, input)
					}
				}
			}
			for _, file := range files {
				if !exists(root, file) {
					continue // a refused or binary-only row writes no text
				}
				if err := bind(manifest, name, unit, table, file); err != nil {
					return nil, err
				}
			}
		}
	}
	all, err := jsonFiles(root, dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, fx := range out {
		seen[fx.file] = true
	}
	for _, file := range all {
		if !seen[file] {
			return nil, fmt.Errorf("%s: no manifest row binds this JSON file to a schema", file)
		}
	}
	return out, nil
}

// gatherJSONIntegerFixtures binds the json-integers vectors to their unit,
// Integers.schema beside the manifests.
func gatherJSONIntegerFixtures(root string) ([]wideFixture, error) {
	const dir = "testdata/wire/json-integers"
	var out []wideFixture
	for _, manifest := range []string{dir + "/MANIFEST.tsv", dir + "/NUMBER-TOKEN.tsv"} {
		rows, err := manifestLines(root, manifest)
		if err != nil {
			return nil, err
		}
		for _, f := range rows {
			out = append(out, wideFixture{schema: dir + "/Integers.schema", root: f[1], file: dir + "/" + f[2], manifest: manifest, row: f[0]})
		}
	}
	return out, nil
}

// gatherCommentRuleFixtures binds the JSON comment vectors, read at
// tables/examples' ProfileConfig (their manifest's header).
func gatherCommentRuleFixtures(root string) ([]wideFixture, error) {
	const dir = "testdata/wire/json-comment-text-rules"
	rows, err := manifestLines(root, dir+"/manifest.tsv")
	if err != nil {
		return nil, err
	}
	var out []wideFixture
	for _, f := range rows {
		out = append(out, wideFixture{schema: "tables/examples", root: "ProfileConfig", file: dir + "/" + f[0], manifest: dir + "/manifest.tsv", row: f[0]})
	}
	return out, nil
}

// gatherHostileCorpusFixtures binds every text-carrier row of the shared
// hostile corpus's manifests to the schema and table the row names.
func gatherHostileCorpusFixtures(root string) ([]wideFixture, error) {
	var out []wideFixture
	for _, manifest := range []string{
		"internal/ci/hostilecorpus/corpus/manifest.tsv",
		"internal/ci/hostilecorpus/corpus/number-token.tsv",
		"internal/ci/hostilecorpus/wide/manifest.tsv",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifest)))
		if err != nil {
			if os.IsNotExist(err) && strings.HasSuffix(manifest, "/number-token.tsv") {
				continue // as above: its exemption line then binds nothing
			}
			return nil, err
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 9 {
				return nil, fmt.Errorf("%s: %q has %d fields, want 9", manifest, line, len(f))
			}
			if f[1] != "text" {
				continue
			}
			out = append(out, wideFixture{schema: f[2], root: f[3], file: filepath.ToSlash(filepath.Join(filepath.Dir(manifest), f[4])), manifest: manifest, row: f[0]})
		}
	}
	return out, nil
}

// gatherFeatureWitnessFixtures binds each feature-witness JSON file to the
// schemas of its own case directory (the nearest directory above it holding a
// .schema). Its case files carry inputs in several harness shapes, so the
// binding is the unit: a unit that declares no wide value cannot hold a bare
// wide integer, and one that does fails until the checker names the root.
func gatherFeatureWitnessFixtures(root string) ([]wideFixture, error) {
	const dir = "testdata/conformance/feature-witnesses"
	files, err := jsonFiles(root, dir)
	if err != nil {
		return nil, err
	}
	var out []wideFixture
	for _, file := range files {
		d := filepath.ToSlash(filepath.Dir(file))
		for {
			schemas, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(d), "*.schema"))
			if err != nil {
				return nil, err
			}
			if len(schemas) > 0 {
				for _, s := range schemas {
					rel, err := filepath.Rel(root, s)
					if err != nil {
						return nil, err
					}
					out = append(out, wideFixture{schema: filepath.ToSlash(rel), file: file, anyRoot: true, manifest: d, row: file})
				}
				break
			}
			if d == dir {
				return nil, fmt.Errorf("%s: no schema in its case directory", file)
			}
			d = filepath.ToSlash(filepath.Dir(d))
		}
	}
	return out, nil
}

// checkTextformOracles holds the text-form vectors' value oracles
// (test/conformance/textform) to the same rule: a field the contract declares
// wide carries its value in a wrapper object (`decimal`, `raw`), never as a
// bare number.
func checkTextformOracles(root string) ([]string, error) {
	const dir = "test/conformance/textform"
	contracts := map[string]map[string]string{}
	for _, name := range []string{"contract.json", "variable-contract.json"} {
		var c struct {
			Root   string            `json:"root"`
			Fields map[string]string `json:"fields"`
		}
		data, err := os.ReadFile(filepath.Join(root, dir, name))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		contracts[c.Root] = c.Fields
	}
	oracles, err := filepath.Glob(filepath.Join(root, dir, "*.value.json"))
	if err != nil {
		return nil, err
	}
	if len(oracles) == 0 {
		return nil, fmt.Errorf("%s: no value oracle", dir)
	}
	var findings []string
	for _, path := range oracles {
		var o struct {
			Root   string                     `json:"root"`
			Fields map[string]json.RawMessage `json:"explicit_fields"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &o); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		fields, ok := contracts[o.Root]
		if !ok {
			return nil, fmt.Errorf("%s: root %s has no contract", path, o.Root)
		}
		for name, raw := range o.Fields {
			if !contractWide(fields[name]) {
				continue
			}
			if tok := bareNumber(raw); tok != "" {
				findings = append(findings, fmt.Sprintf("%s/%s: explicit_fields.%s %s is a bare number for the wide field %q; use its wrapper", dir, filepath.Base(path), name, tok, fields[name]))
			}
		}
	}
	return findings, nil
}

var contractKindRe = regexp.MustCompile(`^(?:[?*]|\[[^\]]*\])*(u?int(\d+)|bits\((\d+)\)|u?fixed\((\d+),\s*(\d+)\))`)

func contractWide(kind string) bool {
	m := contractKindRe.FindStringSubmatch(kind)
	if m == nil {
		return false
	}
	for _, g := range []string{m[2], m[3]} {
		if g != "" {
			n, _ := strconv.Atoi(g)
			return n > 32
		}
	}
	i, _ := strconv.Atoi(m[4])
	f, _ := strconv.Atoi(m[5])
	return i+f > 32
}

// bareNumber returns the first number token in a value or its array, or "".
func bareNumber(raw json.RawMessage) string {
	if t := strings.TrimSpace(string(raw)); t != "" && numberByte(t[0]) {
		return t
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		for _, e := range list {
			if tok := bareNumber(e); tok != "" {
				return tok
			}
		}
	}
	return ""
}
