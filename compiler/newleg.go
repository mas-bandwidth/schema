package compiler

import (
	"fmt"
	"go/format"
	"go/token"
	"path"
	"sort"
	"strings"
)

// NewLeg returns the scaffolding for a new language backend, keyed by path
// relative to the repository root. It writes nothing: the CLI verb
// `schema new-leg` is the writer. The files are the discovered-registry
// skeleton (docs/CONTRIBUTING.md, "Adding a language") — one target, one
// emitter with a passing fixture, make/<lang>.mk, a conformance driver and
// a tables-bench leg — not a codec.
func NewLeg(lang string) (map[string][]byte, error) {
	if err := checkNewLegName(lang); err != nil {
		return nil, err
	}
	ext, comment := newLegRecipe(lang)
	vals := map[string]string{
		"LANG":    lang,
		"TARGET":  lang + "Target",
		"TAG":     "schema_leg_" + lang,
		"EXT":     ext,
		"COMMENT": comment,
		"LANGUP":  strings.ToUpper(lang),
	}
	files := map[string][]byte{
		path.Join("compiler", newLegTargetFile(lang)):           []byte(renderNewLeg(newLegTargetSrc, vals)),
		path.Join("internal", "codegen", lang, lang+".go"):      []byte(renderNewLeg(newLegEmitterSrc, vals)),
		path.Join("internal", "codegen", lang, lang+"_test.go"): []byte(renderNewLeg(newLegEmitterTestSrc, vals)),
		path.Join("make", lang+".mk"):                           []byte(renderNewLeg(newLegMakeSrc, vals)),
		path.Join("test", "conformance", lang, "driver"):        []byte(renderNewLeg(newLegDriverSrc, vals)),
		path.Join("test", "conformance", lang, "ci.json"):       []byte(renderNewLeg(newLegCISrc, vals)),
		path.Join("bench", "tables", lang, "leg"):               []byte(renderNewLeg(newLegBenchSrc, vals)),
	}
	if lang == "lua" {
		files[path.Join("test", lang, "main.lua")] = []byte(newLegLuaMainSrc)
	}
	out := make(map[string][]byte, len(files))
	for name, data := range files {
		if strings.HasSuffix(name, ".go") {
			formatted, err := format.Source(data)
			if err != nil {
				return nil, fmt.Errorf("scaffolded %s does not parse: %w", name, err)
			}
			data = formatted
		}
		if !strings.HasSuffix(string(data), "\n") {
			data = append(data, '\n')
		}
		out[name] = data
	}
	return out, nil
}

// NewLegExec reports whether a NewLeg path must be executable: the
// conformance driver and the tables-bench leg, matching the planted
// language in the registry gate.
func NewLegExec(rel string) bool {
	base := path.Base(rel)
	return base == "driver" || base == "leg"
}

func checkNewLegName(lang string) error {
	if lang == "" {
		return fmt.Errorf("new-leg needs the language name: schema new-leg lua")
	}
	if err := identNewLeg(lang); err != nil {
		return err
	}
	for _, g := range builtins() {
		for _, n := range g.Names() {
			if n == lang {
				return fmt.Errorf("new-leg %s: %s is already a live target (%s)", lang, lang, englishList(New().Targets()))
			}
		}
	}
	if token.IsKeyword(lang) {
		return fmt.Errorf("new-leg needs a language name like lua, not the Go keyword %q", lang)
	}
	if goosOrGoarch[lang] {
		return fmt.Errorf("new-leg %s: %s is a Go GOOS/GOARCH, and compiler/target_%s.go would vanish from every other build", lang, lang, lang)
	}
	if reservedNewLeg[lang] {
		return fmt.Errorf("new-leg %s: %s is reserved by this tree", lang, lang)
	}
	return nil
}

func identNewLeg(lang string) error {
	if lang == "" || lang[0] < 'a' || lang[0] > 'z' {
		return fmt.Errorf("new-leg needs a lowercase identifier (lua), not %q", lang)
	}
	for _, r := range lang {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		return fmt.Errorf("new-leg needs a lowercase identifier (lua), not %q", lang)
	}
	return nil
}

func newLegRecipe(lang string) (ext, comment string) {
	if lang == "lua" {
		return "lua", "--"
	}
	return lang, "#"
}

func newLegTargetFile(lang string) string {
	return "target_" + lang + ".go"
}

func renderNewLeg(tmpl string, vals map[string]string) string {
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	out := tmpl
	for _, k := range keys {
		out = strings.ReplaceAll(out, "{{"+k+"}}", vals[k])
	}
	return out
}

// goosOrGoarch is every GOOS/GOARCH that would make compiler/target_<lang>.go
// a build-constrained file (the JavaScript target is named target_javascript.go
// for this reason).
var goosOrGoarch = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true,
	"freebsd": true, "hurd": true, "illumos": true, "ios": true, "js": true,
	"linux": true, "nacl": true, "netbsd": true, "openbsd": true, "plan9": true,
	"solaris": true, "wasip1": true, "windows": true, "zos": true,
	"386": true, "amd64": true, "arm": true, "arm64": true, "loong64": true,
	"mips": true, "mips64": true, "mips64le": true, "mipsle": true,
	"ppc64": true, "ppc64le": true, "riscv64": true, "s390x": true, "sparc64": true,
	"wasm": true,
}

var reservedNewLeg = map[string]bool{
	"checks": true, "compiler": true, "internal": true, "ir": true,
	"schema": true, "testdata": true, "vendor": true,
}

const newLegTargetSrc = `//go:build {{TAG}}

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by ` + "`schema new-leg {{LANG}}`" + `. The {{TAG}} tag keeps this
// target out of the default nine until the packet surface has claims in
// compiler/*_test.go; ` + "`go build -tags {{TAG}}`" + ` is ` + "`schema generate --lang {{LANG}}`" + `.
// Drop the tag when the port joins the builtins.
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/{{LANG}}"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type {{TARGET}} struct{}

func ({{TARGET}}) Names() []string { return []string{"{{LANG}}"} }

func ({{TARGET}}) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := ir.RefuseWideTableKinds(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	if err := refuseWideText(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	if err := refuseTables(u, "{{LANG}}"); err != nil {
		return nil, err
	}
	return {{LANG}}.Generate(u)
}

func init() {
	registerBuiltin({{TARGET}}{}, false, false, false, false)
}
`

const newLegEmitterSrc = `// Package {{LANG}} emits the {{LANG}} target: one .{{EXT}} file per schema file —
// Constants.schema -> Constants.{{EXT}} — deterministic to the byte (SPEC §6.1).
//
// Scaffolded by ` + "`schema new-leg {{LANG}}`" + `. This is a PACKET STUB, not a codec:
// it writes the package name and protocol id so the fixture can load, and
// refuses nothing the compiler target did not already refuse. Replace
// Generate by walking ir.Unit (docs/CONTRIBUTING.md, "Adding a language").
package {{LANG}}

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "{{EXT}}"

// Generate returns basename.{{EXT}} -> file contents for every file of the unit.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, f := range u.Files {
		var b strings.Builder
		fmt.Fprintf(&b, "{{COMMENT}} Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", f.Base)
		fmt.Fprintf(&b, "{{COMMENT}} Packet skeleton for the {{LANG}} target. Replace Generate in\n")
		fmt.Fprintf(&b, "{{COMMENT}} internal/codegen/{{LANG}}/{{LANG}}.go by walking ir.Unit.\n\n")
		if ext == "lua" {
			fmt.Fprintf(&b, "return {\n")
			fmt.Fprintf(&b, "  package = %q,\n", u.Package)
			fmt.Fprintf(&b, "  protocol_id = %q,\n", fmt.Sprintf("0x%016x", u.ProtocolId))
			fmt.Fprintf(&b, "  file = %q,\n", f.Base)
			fmt.Fprintf(&b, "}\n")
		} else {
			fmt.Fprintf(&b, "package: %s\n", u.Package)
			fmt.Fprintf(&b, "protocol_id: 0x%016x\n", u.ProtocolId)
			fmt.Fprintf(&b, "file: %s\n", f.Base)
		}
		out[f.Base+"."+ext] = []byte(b.String())
	}
	return out, nil
}
`

const newLegEmitterTestSrc = `package {{LANG}}

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestSkeletonFixture is the one passing fixture ` + "`schema new-leg {{LANG}}`" + ` lays
// down: a table-free unit generates, and the file names the package.
func TestSkeletonFixture(t *testing.T) {
	u := &ir.Unit{
		Package:    "hello",
		ProtocolId: 0x11,
		Files:      []*ir.File{{Base: "Hello"}},
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	name := "Hello." + ext
	got, ok := files[name]
	if !ok {
		t.Fatalf("Generate omitted %s; emitted %v", name, keysOf(files))
	}
	text := string(got)
	for _, want := range []string{
		"Code generated by the schema compiler from Hello.schema",
		"hello",
		"protocol_id",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if ext != "lua" {
		return
	}
	lua, err := exec.LookPath("lua")
	if err != nil {
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), got, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := filepath.Join(dir, "run.lua")
	if err := os.WriteFile(runner, []byte("local m = assert(loadfile(\"Hello.lua\"))()\nassert(type(m) == \"table\")\nassert(m.package == \"hello\", m.package)\nassert(type(m.protocol_id) == \"string\" and m.protocol_id:match(\"^0x%x+$\"), m.protocol_id)\nprint(\"ok\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(lua, runner)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lua fixture: %v\n%s", err, out)
	}
}

func keysOf(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	return out
}
`

const newLegMakeSrc = `# make/{{LANG}}.mk — the {{LANGUP}} leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.
#
# Scaffolded by ` + "`schema new-leg {{LANG}}`" + `. The emitter is a packet stub:
# ` + "`make test-{{LANG}}`" + ` is the fixture. Drop the {{TAG}} build tag on
# compiler/target_{{LANG}}.go so ` + "`schema generate --lang {{LANG}}`" + ` joins
# the builtins.

.PHONY: test-{{LANG}} update-goldens-{{LANG}}
test-{{LANG}}:
	go test ./internal/codegen/{{LANG}} -count=1
update-goldens-{{LANG}}: ;
build/conformance-{{LANG}}: ;
generated/bench/tables/{{LANG}}/.stamp: ;

TEST_LEGS         += test-{{LANG}}
CONFORMANCE_LEGS  += $(call unless_skipped,{{LANG}},build/conformance-{{LANG}})
BENCH_TABLES_LEGS += generated/bench/tables/{{LANG}}/.stamp
GOLDENS_LEGS      += update-goldens-{{LANG}}
`

const newLegDriverSrc = `#!/bin/sh
# THE {{LANGUP}} LEG's driver (test/conformance/README.md).
#
# Scaffolded by ` + "`schema new-leg {{LANG}}`" + `. A driver is a command, not a
# binary: list the surfaces this backend implements, one per line; a surface
# this skeleton does not yet answer is exit 2 (ABSENT).
#
# The working directory is the repository root.
#
# usage: driver <manifest> list
#        driver <manifest> <surface> <outdir>
case "$2" in
list)
	# no surfaces yet — the matrix stays empty until Generate answers one
	exit 0
	;;
*)
	exit 2
	;;
esac
`

const newLegCISrc = `{"targets": "build/conformance-harness build/conformance-{{LANG}}"}
`

const newLegBenchSrc = `#!/bin/sh
# THE {{LANGUP}} LEG of the tables bench (bench/tables/README.md).
# Scaffolded by ` + "`schema new-leg {{LANG}}`" + `.
set -e
case "$1" in
build)
	exit 0
	;;
run)
	echo '{{LANG}},bench_table,write,1,1,1,1,1,1,1,0,0,table,pkg,contract,default,unknown'
	echo '{{LANG}},bench_table,round_trip,1,1,1,1,1,1,1,0,0,table,pkg,contract,default,unknown'
	;;
*)
	echo "usage: $0 build | run [args...]" >&2
	exit 1
	;;
esac
`

const newLegLuaMainSrc = `-- THE LUA LEG's interpreter harness (docs/CONTRIBUTING.md, "Adding a language").
-- Load a generated stub module and require the package name it emits.
local path = assert(arg[1], "usage: lua test/lua/main.lua <generated.lua>")
local m = assert(loadfile(path))()
assert(type(m) == "table", "generated file must return a table")
assert(type(m.package) == "string" and m.package ~= "", "package")
assert(type(m.protocol_id) == "string" and m.protocol_id:match("^0x%x+$"), "protocol_id")
print("ok")
`
