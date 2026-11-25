// singlepasscheck holds ccommon.SinglePassConditionals to what it rewrote:
// for every C and C++ file the table generators wrote, the form as emitted
// (SCHEMA_CONDITIONALS_AS_EMITTED=1) and the rewritten form must preprocess
// to the same tokens under every configuration in Configs. It is the engine
// of make tables-single-pass-equivalence.
//
//	singlepasscheck BEFORE AFTER
//
// BEFORE and AFTER hold the same trees, one directory per generated tree,
// named for its language: a directory ending in -c is C, one ending in -cpp
// is C++. Every file under one must be under the other. A file that differs
// is named, with the configuration and the tokens where the two part; the
// exit status is 1 on any difference and 2 on a usage or reading error.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: singlepasscheck BEFORE AFTER")
		os.Exit(2)
	}
	problems, files, rewritten, err := checkTrees(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "SINGLE-PASS EQUIVALENCE FAILED:", err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Println("SINGLE-PASS EQUIVALENCE FAILED:", p)
	}
	if len(problems) > 0 {
		fmt.Printf("single-pass equivalence: %d of %d files differ\n", len(problems), files)
		os.Exit(1)
	}
	fmt.Printf("single-pass equivalence: %d generated C and C++ files, %d of them rewritten, preprocess to the same tokens as emitted under %d configurations\n", files, rewritten, len(Configs))
}

// treeLang is the language of a generated tree, from its directory's name.
func treeLang(dir string) (string, error) {
	switch {
	case strings.HasSuffix(dir, "-cpp"):
		return "c++", nil
	case strings.HasSuffix(dir, "-c"):
		return "c", nil
	}
	return "", fmt.Errorf("%s: a tree's directory ends in -c or -cpp", dir)
}

func listFiles(root string) (map[string]bool, error) {
	files := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".h", ".c", ".cpp", ".hpp", ".cc", ".inc":
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			files[rel] = true
		}
		return nil
	})
	return files, err
}

// checkTrees compares every file of every tree and returns one problem per
// file that differs or is missing on one side, sorted by name.
func checkTrees(before, after string) (problems []string, files, rewritten int, err error) {
	trees, err := os.ReadDir(after)
	if err != nil {
		return nil, 0, 0, err
	}
	type job struct{ rel, lang string }
	var jobs []job
	for _, t := range trees {
		if !t.IsDir() {
			continue
		}
		lang, err := treeLang(t.Name())
		if err != nil {
			return nil, 0, 0, err
		}
		fb, err := listFiles(filepath.Join(before, t.Name()))
		if err != nil {
			return nil, 0, 0, err
		}
		fa, err := listFiles(filepath.Join(after, t.Name()))
		if err != nil {
			return nil, 0, 0, err
		}
		if len(fa) == 0 {
			return nil, 0, 0, fmt.Errorf("%s: no generated C or C++ file", filepath.Join(after, t.Name()))
		}
		for rel := range fb {
			if !fa[rel] {
				problems = append(problems, fmt.Sprintf("%s: written as emitted, missing rewritten", filepath.Join(t.Name(), rel)))
			}
		}
		for rel := range fa {
			if !fb[rel] {
				problems = append(problems, fmt.Sprintf("%s: written rewritten, missing as emitted", filepath.Join(t.Name(), rel)))
				continue
			}
			jobs = append(jobs, job{filepath.Join(t.Name(), rel), lang})
		}
	}
	if len(jobs) == 0 {
		return nil, 0, 0, fmt.Errorf("%s: no generated tree to compare", after)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, j := range jobs {
		b, err := os.ReadFile(filepath.Join(before, j.rel))
		if err != nil {
			return nil, 0, 0, err
		}
		a, err := os.ReadFile(filepath.Join(after, j.rel))
		if err != nil {
			return nil, 0, 0, err
		}
		files++
		// THE SAME BYTES PREPROCESS TO THE SAME TOKENS: a file the rewrite
		// left as it was (no #else or #elif in it) has nothing to compare
		if string(a) == string(b) {
			continue
		}
		rewritten++
		wg.Add(1)
		sem <- struct{}{}
		go func(rel, lang, b, a string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := Compare(b, a, lang); err != nil {
				mu.Lock()
				problems = append(problems, rel+": "+err.Error())
				mu.Unlock()
			}
		}(j.rel, j.lang, string(b), string(a))
	}
	wg.Wait()
	sort.Strings(problems)
	return problems, files, rewritten, nil
}
