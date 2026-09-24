package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"io/fs"
	"os"
	"path"
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
	var outs []planned
	for _, f := range files {
		data, err := render(f.template, l)
		if err != nil {
			return nil, err
		}
		outs = append(outs, planned{f.path(l), data})
	}
	return write(root, outs)
}

// planned is one rendered file and where it lands, slash-separated and
// relative to the checkout.
type planned struct {
	rel  string
	data []byte
}

// beforeCreate runs between the preflight and each file's creation; it is nil
// outside the tests, which use it to race a file in where one is about to land.
var beforeCreate func(rel string)

// write lays the planned files into root, confined to it. Every output goes
// through an os.Root, so no path — a symlink planted in the tree or raced in
// later included — resolves outside --root; a preflight refuses, by name, any
// output whose path is or passes through a symlink, and any file that already
// exists, before anything is written; and each file is then created with
// O_CREATE|O_EXCL, so one that appears after the preflight (a file or a
// dangling symlink alike) is refused rather than written through. Only "does
// not exist" counts as free: any other lookup error is reported. A failure
// part-way removes what this run created, so a refusal leaves the tree as it
// found it.
func write(root string, outs []planned) (written []string, err error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	for _, o := range outs {
		if err := preflight(r, o.rel); err != nil {
			return nil, err
		}
	}
	var made []string // directories this run created, outermost first
	defer func() {
		if err == nil {
			return
		}
		for i := len(written) - 1; i >= 0; i-- {
			_ = r.Remove(filepath.FromSlash(written[i]))
		}
		for i := len(made) - 1; i >= 0; i-- {
			_ = r.Remove(filepath.FromSlash(made[i])) // fails, harmlessly, on one not empty
		}
		written = nil
	}()
	for _, o := range outs {
		if err := mkdirs(r, path.Dir(o.rel), &made); err != nil {
			return written, err
		}
		if beforeCreate != nil {
			beforeCreate(o.rel)
		}
		f, err := r.OpenFile(filepath.FromSlash(o.rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			return written, fmt.Errorf("%s appeared while new-leg was writing — it never overwrites a file", o.rel)
		}
		if err != nil {
			return written, fmt.Errorf("%s: %w", o.rel, err)
		}
		written = append(written, o.rel)
		_, werr := f.Write(o.data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return written, fmt.Errorf("%s: %w", o.rel, werr)
		}
	}
	return written, nil
}

// preflight walks rel one element at a time with Lstat, so no link is
// followed: a symlink anywhere on the path is refused, as is a file where a
// directory goes and a final file that already exists; the first element that
// does not exist ends the walk (everything below it is free too).
func preflight(r *os.Root, rel string) error {
	elems := strings.Split(rel, "/")
	for i := range elems {
		p := strings.Join(elems[:i+1], "/")
		info, err := r.Lstat(filepath.FromSlash(p))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: cannot tell whether it exists: %w", rel, err)
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("%s is a symlink — new-leg writes %s only as a real file under --root and never through a link", p, rel)
		case i == len(elems)-1:
			return fmt.Errorf("%s already exists — new-leg lays down a new leg and never overwrites a file", rel)
		case !info.IsDir():
			return fmt.Errorf("%s is not a directory, and %s goes under it", p, rel)
		}
	}
	return nil
}

// mkdirs creates dir and its missing parents inside r, one element at a time,
// recording each it created; an element that is already there must be a real
// directory (a symlink raced in after the preflight is refused).
func mkdirs(r *os.Root, dir string, made *[]string) error {
	if dir == "." {
		return nil
	}
	elems := strings.Split(dir, "/")
	for i := range elems {
		p := strings.Join(elems[:i+1], "/")
		err := r.Mkdir(filepath.FromSlash(p), 0o755)
		if err == nil {
			*made = append(*made, p)
			continue
		}
		if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s: %w", p, err)
		}
		info, err := r.Lstat(filepath.FromSlash(p))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is not a real directory — new-leg never writes through a link", p)
		}
	}
	return nil
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
