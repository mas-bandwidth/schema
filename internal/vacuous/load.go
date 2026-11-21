package vacuous

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// A srcFile is one parsed .go file.
type srcFile struct {
	path    string // relative to the tree root, slash-separated
	ast     *ast.File
	isTest  bool
	imports map[string]string // local name -> import path
	pkg     *pkgDir
}

// A pkgDir is every .go file of one directory, test and non-test, whatever the
// build constraints say: a helper behind a build tag is still a helper.
type pkgDir struct {
	dir     string // relative to the tree root; "." for the root
	files   []*srcFile
	funcs   map[string][]*ast.FuncDecl
	methods map[string][]*ast.FuncDecl
	vars    map[string]ast.Expr // package-level var or const -> its initializer (nil when it has none)
}

// A tree is the parsed repository: every directory holding a _test.go file,
// plus every in-module directory such a file imports (loaded on demand so a
// helper in another package is followed, not guessed at).
type tree struct {
	root    string
	fset    *token.FileSet
	modules map[string]string // module path -> directory relative to root
	mu      sync.Mutex
	pkgs    map[string]*pkgDir
	declAt  map[*ast.FuncDecl]*srcFile

	testFiles int // _test.go files parsed
	testFuncs int // Test, Fuzz and Benchmark functions seen
}

// skipDir names the directories the walk never enters: fixtures under
// testdata are inputs to tests, not tests, and .git is not source.
func skipDir(name string) bool {
	return name == "testdata" || name == ".git" || name == "node_modules" || name == "vendor"
}

// notThisTree reports a directory below the root whose tests are not this
// repository's: the root's build/ (ignored output, where make clones other
// repositories and writes generated trees) and any nested repository (a
// directory holding its own .git). Their tests are another repository's to
// gate; counting them would make this gate red or green on what a bench
// happens to hold.
func notThisTree(path, rel string) bool {
	if rel == "build" {
		return true
	}
	_, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil
}

func loadTree(root string) (*tree, error) {
	t := &tree{
		root:    root,
		fset:    token.NewFileSet(),
		modules: map[string]string{},
		pkgs:    map[string]*pkgDir{},
		declAt:  map[*ast.FuncDecl]*srcFile{},
	}
	var testDirs []string
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if path != root && (skipDir(d.Name()) || notThisTree(path, rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		switch {
		case d.Name() == "go.mod":
			mod, err := modulePath(path)
			if err != nil {
				return err
			}
			t.modules[mod] = dir
		case strings.HasSuffix(d.Name(), "_test.go"):
			if !seen[dir] {
				seen[dir] = true
				testDirs = append(testDirs, dir)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(testDirs)
	for _, dir := range testDirs {
		if _, err := t.load(dir); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func modulePath(gomod string) (string, error) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no module line", gomod)
}

// load parses one directory. A directory that does not exist or holds no .go
// file loads as an empty package: an import that resolves to nothing is an
// unknown callee, which the rules treat as such.
func (t *tree) load(dir string) (*pkgDir, error) {
	t.mu.Lock()
	if p, ok := t.pkgs[dir]; ok {
		t.mu.Unlock()
		return p, nil
	}
	p := &pkgDir{dir: dir, funcs: map[string][]*ast.FuncDecl{}, methods: map[string][]*ast.FuncDecl{}, vars: map[string]ast.Expr{}}
	t.pkgs[dir] = p
	t.mu.Unlock()

	entries, err := os.ReadDir(filepath.Join(t.root, filepath.FromSlash(dir)))
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			names = append(names, e.Name())
		}
	}
	files := make([]*srcFile, len(names))
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, name := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rel := name
			if dir != "." {
				rel = dir + "/" + name
			}
			af, err := parser.ParseFile(t.fset, filepath.Join(t.root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				errs[i] = err
				return
			}
			sf := &srcFile{path: rel, ast: af, isTest: strings.HasSuffix(name, "_test.go"), imports: map[string]string{}, pkg: p}
			for _, im := range af.Imports {
				ip, err := strconv.Unquote(im.Path.Value)
				if err != nil {
					continue
				}
				local := ip[strings.LastIndex(ip, "/")+1:]
				if isMajorVersion(local) {
					trimmed := strings.TrimSuffix(ip, "/"+local)
					local = trimmed[strings.LastIndex(trimmed, "/")+1:]
				}
				if im.Name != nil {
					local = im.Name.Name
				}
				sf.imports[local] = ip
			}
			files[i] = sf
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, sf := range files {
		p.files = append(p.files, sf)
		if sf.isTest {
			t.testFiles++
		}
		for _, d := range sf.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				t.declAt[d] = sf
				if d.Recv != nil {
					p.methods[d.Name.Name] = append(p.methods[d.Name.Name], d)
				} else {
					p.funcs[d.Name.Name] = append(p.funcs[d.Name.Name], d)
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR && d.Tok != token.CONST {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, n := range vs.Names {
						var init ast.Expr
						if i < len(vs.Values) {
							init = vs.Values[i]
						} else if len(vs.Values) == 1 {
							init = vs.Values[0]
						}
						p.vars[n.Name] = init
					}
				}
			}
		}
	}
	return p, nil
}

func isMajorVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// importDir resolves an import path to a directory of this tree, or "" when
// the import is the standard library or a third-party module.
func (t *tree) importDir(importPath string) string {
	best, bestDir := "", ""
	for mod, dir := range t.modules {
		if (importPath == mod || strings.HasPrefix(importPath, mod+"/")) && len(mod) > len(best) {
			best, bestDir = mod, dir
		}
	}
	if best == "" {
		return ""
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(importPath, best), "/")
	switch {
	case rest == "":
		return bestDir
	case bestDir == ".":
		return rest
	}
	return bestDir + "/" + rest
}

// imported returns the package a file's local import name refers to, when that
// package is in this tree.
func (t *tree) imported(sf *srcFile, local string) *pkgDir {
	ip, ok := sf.imports[local]
	if !ok {
		return nil
	}
	dir := t.importDir(ip)
	if dir == "" {
		return nil
	}
	p, err := t.load(dir)
	if err != nil {
		return nil
	}
	return p
}

func (t *tree) pos(n ast.Node) (string, int) {
	p := t.fset.Position(n.Pos())
	rel, err := filepath.Rel(t.root, p.Filename)
	if err != nil {
		rel = p.Filename
	}
	return filepath.ToSlash(rel), p.Line
}
