// The C++ leg's name-claim scan, both ways, over the PACKET emitter and the
// TABLE emitter (docs/SPEC-TABLES.md §11, docs/PORTING.md I6, schema #414).
// Written as the other legs' scans are — TestCTableRuntimeNamesAreClaimed,
// TestTableRuntimeNamesAreClaimedGo, TestTableRuntimeNamesAreClaimed (C#) — and
// guarded by TestCppRuntimeNameScanGoesRed (tablescpp_test.go), which keeps the
// collector it reads honest.
package compiler

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablenames"
)

// cppCorpusDirs are the repo's unit directories the scan generates, beside the
// inline sources below. A name that only a larger unit emits (the map's cursors,
// the retain walk, the 128-bit storage) is registered the day it is written or
// the scan fails on it, so the corpus is the whole of what the repo generates.
var cppCorpusDirs = []string{
	"../tables/arms", "../tables/backend", "../tables/blobs", "../tables/block", "../tables/blockhome",
	"../tables/cards", "../tables/examples", "../tables/lists", "../tables/maps", "../tables/messages",
	"../tables/pointers", "../tables/scalars", "../tables/stream", "../tables/vocab", "../tables/vocab9",
	"../examples", "../examples-wide", "../examples128",
}

// cppCorpusSources are the inline units: the fixed runtime, a pointered one,
// the census-gated constructs (wide text, a float64 rung, the byte buffer), and
// the unbounded array, so every gated block of the emitter is written at least
// once.
var cppCorpusSources = []string{
	runtimeSrc,
	cRuntimeSrc,
	cCensusRuntimeSrc,
	allVariableSrc,
	goRuntimeSrc,
	goRuntimeSrc + "\ntable Graph {head *Graph\ndata *bytes\ncaption *string\n}\n",
	goRuntimeSrc + "\ntable Lists {rows []int32}\n",
	goRuntimeSrc + "\ntable Wide {label wstring(16)\n}\n",
	goRuntimeSrc + "\ntable Drift {ratio float64\n}\n",
}

// cppCorpus generates every corpus unit for the cpp target and returns the
// union of the emitted files, each keyed by the unit it came from so two units'
// same-named files do not overwrite one another. The packet files and the table
// files are both in it; the two scans below read different halves.
func cppCorpus(t *testing.T) (files map[string][]byte, packages map[string]string) {
	t.Helper()
	files = map[string][]byte{}
	packages = map[string]string{}
	add := func(label string, out map[string][]byte, pkg string) {
		for name, data := range out {
			key := fmt.Sprintf("%s/%s", label, name)
			files[key] = data
			packages[key] = pkg
		}
	}
	for i, src := range cppCorpusSources {
		u := unitFromSource(t, src)
		out, err := New().Generate(u, "cpp", Options{})
		if err != nil {
			t.Fatalf("--lang cpp over inline unit %d: %v", i, err)
		}
		add(fmt.Sprintf("src%d", i), out, u.Package)
	}
	for _, dir := range cppCorpusDirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
		if err != nil || len(paths) == 0 {
			t.Fatalf("no schemas under %s: %v", dir, err)
		}
		c := New()
		u, err := c.Load(paths)
		if err != nil {
			t.Fatalf("load %s: %v", dir, err)
		}
		out, err := c.Generate(u, "cpp", Options{})
		if err != nil {
			t.Fatalf("--lang cpp over %s: %v", dir, err)
		}
		add(filepath.Base(dir), out, u.Package)
	}
	return files, packages
}

// TestCppTableRuntimeNamesAreClaimed is the C++ leg's half of the §11 promise,
// and it is the twin of TestCTableRuntimeNamesAreClaimed over the C++ table
// emitter's output. C++ has a namespace, but a unit's schema names land in the
// SAME one (`namespace <package>`), so every name the runtime spells at
// namespace scope is one a schema could collide with. Both directions:
//
//   - EVERY name the emitted Table and Block sources spell at namespace scope
//     is registered, and registered FOR THIS BACKEND. A runtime name somebody
//     added to the emitter and forgot to register fails here by name; so does
//     one registered only for another backend, because the registry's By is
//     what a diagnostic tells the author (tablenames.By).
//   - EVERY name the registry says the C++ backend defines is spelled by the
//     emitted C++. A claim nothing needs takes a name away from every schema
//     for free.
//
// What it reads is cppEmittedNames: every Table*/kTable*/table_* identifier,
// the announcement vocabulary and BuildVersion wherever they appear, plus the
// enumerators of the unscoped enums and the lowercase constants the refusal
// vocabulary is declared in. The scan recognises no declaration syntax, so the
// only mistake it can make is to over-collect, and that mistake is safe.
func TestCppTableRuntimeNamesAreClaimed(t *testing.T) {
	files, _ := cppCorpus(t)
	emitted := cppEmittedNames(files)
	if len(emitted) == 0 {
		t.Fatal("the scan found no runtime identifier in the emitted C++ at all — the scan, not the registry, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		switch {
		case !tablenames.Registered(name):
			t.Errorf("the C++ table emitter emits %s and internal/tablenames does not register it — "+
				"a schema declaring that name would generate C++ that does not compile; register it "+
				"(with the backends that define it) in internal/tablenames", name)
		case tablenames.By(name)&tablenames.Cpp == 0:
			t.Errorf("the C++ table emitter emits %s and internal/tablenames registers it for other backends "+
				"only — add the Cpp bit so the claim says who defines it", name)
		}
	}
	for _, name := range tablenames.DefinedBy(tablenames.Cpp) {
		if !emitted[name] {
			t.Errorf("internal/tablenames says the C++ backend defines %s, but nothing in the emitted "+
				"C++ names it — drop the registration or fix the backend; a claim nothing needs takes "+
				"a name away from every schema for free", name)
		}
	}
	t.Logf("the C++ table scan saw %d runtime names over %d generated files, %d registered for this backend",
		len(emitted), len(files), len(tablenames.DefinedBy(tablenames.Cpp)))
}

// cppPacketIdent is the PACKET emitter's scan: every name its headers spell
// that is not derived from a schema declaration — the macros (SCHEMA_*), the
// lowercase helpers (schema_*), the flag-name appenders, EnumName and
// ProtocolId. It reads the files the table scan does not (<Base>.h and
// <Base>Wire.h) and is shape independent: a name is collected wherever it
// appears. The per-declaration spellings (FlagName<Flags>, <Type>Reset, ...)
// are the author's own names and claim nothing here.
var cppPacketIdent = regexp.MustCompile(`\b(?:SCHEMA_[A-Z0-9_]+|schema_[a-z0-9_]+|SchemaFlagAppend[A-Za-z]*_|EnumName|ProtocolId)\b`)

// cppPacketEmittedNames collects cppPacketIdent over the PACKET files only,
// with line and block comments stripped, and folds each unit's upper-cased
// package out of the spelling so the per-package macros read the same in every
// unit.
func cppPacketEmittedNames(files map[string][]byte, packages map[string]string) map[string]bool {
	emitted := map[string]bool{}
	for key, data := range files {
		name := key[strings.Index(key, "/")+1:]
		if strings.HasSuffix(name, "Table.h") || strings.HasSuffix(name, "Table.cpp") ||
			strings.HasSuffix(name, "Block.h") || strings.HasSuffix(name, "Block.cpp") ||
			strings.HasSuffix(name, "View.h") || strings.HasSuffix(name, "View.cpp") {
			continue
		}
		pkg := strings.ToUpper(packages[key])
		for line := range strings.SplitSeq(stripCComments(string(data)), "\n") {
			if i := strings.Index(line, "//"); i >= 0 {
				line = line[:i]
			}
			for _, m := range cppPacketIdent.FindAllString(line, -1) {
				if pkg != "" {
					m = strings.Replace(m, "SCHEMA_"+pkg+"_", "SCHEMA_<PKG>_", 1)
				}
				emitted[m] = true
			}
		}
	}
	return emitted
}

// cppPacketVocabulary is every fixed name the C++ PACKET emitter spells, with
// how the checker holds it today. The package is folded to <PKG>.
//
//   - claimedByPrefix: the lowercase `schema_` prefix, refused for every
//     declaration in every unit (internal/check reserveCPrefix);
//   - claimedByName: refused by its own entry (ProtocolId);
//   - owed: spelled by the emitter, and a schema may declare it without a word
//     from the checker. The list is a CEILING that may only shrink: a name the
//     checker starts refusing must be struck (the test says so), and a name the
//     emitter starts spelling that is in neither list fails by name. The macros
//     are the dangerous ones — a macro is a SILENT REWRITE of a declaration of
//     the same spelling (internal/check, cReservedMacros says it for C) — and
//     this is the C++ packet emitter's remainder of schema#414.
var cppPacketVocabulary = map[string]string{
	"ProtocolId":                         "claimedByName",
	"schema_interior_null":               "claimedByPrefix",
	"schema_read_object":                 "claimedByPrefix",
	"schema_utf8_valid":                  "claimedByPrefix",
	"schema_write_object":                "claimedByPrefix",
	"EnumName":                           "owed",
	"SchemaFlagAppendHex_":               "owed",
	"SchemaFlagAppend_":                  "owed",
	"SCHEMA_OBJECT_MACROS_DEFINED":       "owed",
	"SCHEMA_READ_INLINE":                 "owed",
	"SCHEMA_READ_INLINE_DEFINED":         "owed",
	"SCHEMA_WRITE_INLINE":                "owed",
	"SCHEMA_WRITE_INLINE_DEFINED":        "owed",
	"SCHEMA_<PKG>_FLAG_APPEND_DEFINED":   "owed",
	"SCHEMA_<PKG>_INTERIOR_NULL_DEFINED": "owed",
	"SCHEMA_<PKG>_UTF8_VALID_DEFINED":    "owed",
}

// TestCppPacketRuntimeNamesAreAccountedFor is the packet half of the scan, in
// both directions: every fixed name the C++ packet emitter spells is in
// cppPacketVocabulary, every name the vocabulary lists is spelled, and what the
// vocabulary says the checker does about each one is what the checker does.
func TestCppPacketRuntimeNamesAreAccountedFor(t *testing.T) {
	files, packages := cppCorpus(t)
	emitted := cppPacketEmittedNames(files, packages)
	if len(emitted) == 0 {
		t.Fatal("the scan found no fixed name in the emitted C++ packet headers at all — the scan, not the vocabulary, is what broke")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := cppPacketVocabulary[name]; !ok {
			t.Errorf("the C++ packet emitter spells %s and cppPacketVocabulary does not account for it — "+
				"a schema declaring that name may generate C++ that does not compile; claim it in internal/check "+
				"and list it here", name)
		}
	}
	vocab := make([]string, 0, len(cppPacketVocabulary))
	for name := range cppPacketVocabulary {
		vocab = append(vocab, name)
	}
	sort.Strings(vocab)
	owed := 0
	for _, name := range vocab {
		status := cppPacketVocabulary[name]
		if !emitted[name] {
			t.Errorf("cppPacketVocabulary lists %s, but nothing in the emitted C++ packet headers spells it — "+
				"drop it; a name nothing needs takes a name away from every schema for free", name)
		}
		// the PACKAGE a probe declares, so the folded macros read as real ones
		declared := strings.Replace(name, "<PKG>", "PROBE", 1)
		refused := len(checkErrors(t, "package probe\n\ntype "+declared+"\n{\n    a uint8\n}\n")) > 0
		switch {
		case status == "owed" && refused:
			t.Errorf("%s is recorded as owed, but the checker now refuses it — strike it from the owed list (mark it claimed)", name)
		case status != "owed" && !refused:
			t.Errorf("%s is recorded as %s, but a schema declaring it is accepted — the claim is gone", name, status)
		}
		if status == "owed" {
			owed++
		}
	}
	t.Logf("the C++ packet scan saw %d fixed names, %d of them owed a claim in internal/check", len(emitted), owed)
}
