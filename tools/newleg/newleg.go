package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// probe is a generator that answers to one name and emits nothing; it exists
// only to ask the driver whether that name is free.
type probe string

func (p probe) Names() []string { return []string{string(p)} }

func (probe) Generate(*ir.Unit, compiler.Options) (map[string][]byte, error) { return nil, nil }

//go:embed templates/*.tmpl
var templates embed.FS

// legName is a leg's name: it is a Go package name, a make target suffix, a
// directory and a --lang spelling at once, so it takes the narrowest of the
// four.
var legName = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

// comments is the line-comment opener for the languages a port is likely to
// name; anything else takes // and the flag.
var comments = map[string]string{
	"lua": "--", "haskell": "--", "sql": "--", "ada": "--",
	"python": "#", "ruby": "#", "perl": "#", "nim": "#", "crystal": "#", "julia": "#", "r": "#",
	"erlang": "%", "fortran": "!", "lisp": ";", "clojure": ";",
}

// leg is what the templates read.
type leg struct {
	Lang    string // the --lang spelling and the package name
	Ext     string // ".lua"
	Comment string // "--"
}

// file is one template and where it lands, relative to the checkout.
type file struct {
	template string
	path     func(l leg) string
}

var files = []file{
	{"target.go.tmpl", func(l leg) string { return "compiler/target_" + l.Lang + ".go" }},
	{"backend.go.tmpl", func(l leg) string { return "internal/codegen/" + l.Lang + "/" + l.Lang + ".go" }},
	{"backend_test.go.tmpl", func(l leg) string { return "internal/codegen/" + l.Lang + "/" + l.Lang + "_test.go" }},
	{"leg.mk.tmpl", func(l leg) string { return "make/" + l.Lang + ".mk" }},
	{"Fixture.schema.tmpl", func(l leg) string { return "test/" + l.Lang + "/Fixture.schema" }},
}

// Scaffold writes a new leg's skeleton under root and returns the paths it
// wrote, slash-separated and relative to root. It refuses — writing nothing —
// a name that is no leg name, a name a registered target already answers to,
// and a tree where any of the files, or the leg's make file, already exists.
func Scaffold(root, lang, ext, comment string) ([]string, error) {
	if !legName.MatchString(lang) {
		return nil, fmt.Errorf("leg name %q must be a Go package name: 1-16 lower-case letters and digits, starting with a letter", lang)
	}
	// the driver's own rule decides whether the name is taken: Register
	// refuses a name any generator answers to, an alias ("csharp") included
	if err := compiler.New().Register(probe(lang)); err != nil {
		return nil, fmt.Errorf("%s is already a target (--lang %s) — new-leg lays down a new leg and never overwrites one", lang, lang)
	}
	if token.IsKeyword(lang) {
		return nil, fmt.Errorf("leg name %q must be a Go package name, and it is a Go keyword", lang)
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || info.IsDir() {
		return nil, fmt.Errorf("%s is not a schema checkout (no go.mod) — run new-leg from the repository root or pass --root", root)
	}
	l := leg{Lang: lang, Ext: ext, Comment: comment}
	if l.Ext == "" {
		l.Ext = "." + lang
	}
	if !strings.HasPrefix(l.Ext, ".") || strings.ContainsAny(l.Ext, `/\"`+"`") {
		return nil, fmt.Errorf("--ext %q must start with a dot and name no directory", l.Ext)
	}
	if l.Comment == "" {
		l.Comment = comments[lang]
	}
	if l.Comment == "" {
		l.Comment = "//"
	}
	if strings.ContainsAny(l.Comment, "\"\n`") {
		return nil, fmt.Errorf("--comment %q must be one line with no quote", l.Comment)
	}

	// render everything first, so a refusal or a template fault writes nothing
	type out struct {
		rel  string
		data []byte
	}
	var outs []out
	for _, f := range files {
		rel := f.path(l)
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			return nil, fmt.Errorf("%s already exists — new-leg lays down a new leg and never overwrites a file", rel)
		}
		data, err := render(f.template, l)
		if err != nil {
			return nil, err
		}
		outs = append(outs, out{rel, data})
	}
	var written []string
	for _, o := range outs {
		path := filepath.Join(root, filepath.FromSlash(o.rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(path, o.data, 0o644); err != nil {
			return written, err
		}
		written = append(written, o.rel)
	}
	return written, nil
}

// render executes one template; Go output is gofmt'd, so the skeleton lands
// already in the form the lint gate holds the tree to.
func render(name string, l leg) ([]byte, error) {
	t, err := template.New(name).Delims("[[", "]]").ParseFS(templates, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, l); err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, ".go.tmpl") {
		src, err := format.Source(b.Bytes())
		if err != nil {
			return nil, fmt.Errorf("%s renders Go that does not parse: %w", name, err)
		}
		return src, nil
	}
	return b.Bytes(), nil
}
