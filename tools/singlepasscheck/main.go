// singlepasscheck holds ccommon.SinglePassConditionals to what it rewrote:
// for every C and C++ file the table generators wrote, the form as emitted
// (SCHEMA_CONDITIONALS_AS_EMITTED=1) and the rewritten form must preprocess
// to the same tokens under every configuration in Configs, and every branch
// of every chain the rewrite took apart must be kept under one of them. It is
// the engine of make tables-single-pass-equivalence.
//
//	singlepasscheck -trees NAME[,NAME...] BEFORE AFTER
//
// BEFORE and AFTER each hold exactly the named trees, one directory per
// generated tree, named for its language: a directory ending in -c is C, one
// ending in -cpp is C++. Every file under one side must be under the other,
// and every tree must hold at least one file the rewrite changed: a check
// that compared nothing is red, never green. A file that differs is named,
// with the configuration and the tokens where the two part, or with the
// branches no configuration kept; the exit status is 1 on any difference and
// 2 on a usage or reading error.
package main

import (
	"flag"
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
	trees := flag.String("trees", "", "the tree directories BEFORE and AFTER must each hold, comma separated, and no other")
	flag.Parse()
	if flag.NArg() != 2 || *trees == "" {
		fmt.Fprintln(os.Stderr, "usage: singlepasscheck -trees NAME[,NAME...] BEFORE AFTER")
		os.Exit(2)
	}
	res, err := checkTrees(flag.Arg(0), flag.Arg(1), strings.Split(*trees, ","))
	if err != nil {
		fmt.Fprintln(os.Stderr, "SINGLE-PASS EQUIVALENCE FAILED:", err)
		os.Exit(2)
	}
	for _, p := range res.problems {
		fmt.Println("SINGLE-PASS EQUIVALENCE FAILED:", p)
	}
	if len(res.problems) > 0 {
		fmt.Printf("single-pass equivalence: %d problems over %d files\n", len(res.problems), res.files)
		os.Exit(1)
	}
	fmt.Printf("single-pass equivalence: %d generated C and C++ files in %d trees, %d of them rewritten, preprocess to the same tokens as emitted under %d configurations; all %d branches of the %d chains a configuration reaches were kept under one; %d chains sit where no configuration reaches, so none of the configurations compares them\n",
		res.files, len(res.trees), res.rewritten, len(Configs), res.cov.Branches, res.cov.Chains-res.cov.Unreached, res.cov.Unreached)
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

// treeDirs is the set of directories directly under root.
func treeDirs(root string) (map[string]bool, error) {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{}
	for _, e := range ents {
		if e.IsDir() {
			dirs[e.Name()] = true
		}
	}
	return dirs, nil
}

type result struct {
	problems         []string
	trees            []string
	files, rewritten int
	cov              Coverage
}

// checkTrees compares every file of every named tree and returns one problem
// per file that differs or is missing on one side, and per tree the rewrite
// changed nothing in, sorted by name.
func checkTrees(before, after string, want []string) (result, error) {
	var res result
	wanted := map[string]bool{}
	for _, t := range want {
		if t == "" || wanted[t] {
			return res, fmt.Errorf("-trees %q: each tree is named once", strings.Join(want, ","))
		}
		if _, err := treeLang(t); err != nil {
			return res, err
		}
		wanted[t] = true
	}
	for _, side := range []string{before, after} {
		dirs, err := treeDirs(side)
		if err != nil {
			return res, err
		}
		for t := range wanted {
			if !dirs[t] {
				return res, fmt.Errorf("%s: the tree %s is missing", side, t)
			}
		}
		for d := range dirs {
			if !wanted[d] {
				return res, fmt.Errorf("%s: %s is not one of the trees named (-trees %s)", side, d, strings.Join(want, ","))
			}
		}
	}
	res.trees = append(res.trees, want...)
	sort.Strings(res.trees)
	type job struct{ tree, rel, lang, before, after string }
	var jobs []job
	perTree := map[string]int{}
	for _, t := range res.trees {
		lang, _ := treeLang(t)
		fb, err := listFiles(filepath.Join(before, t))
		if err != nil {
			return res, err
		}
		fa, err := listFiles(filepath.Join(after, t))
		if err != nil {
			return res, err
		}
		for rel := range fb {
			if !fa[rel] {
				res.problems = append(res.problems, fmt.Sprintf("%s: written as emitted, missing rewritten", filepath.Join(t, rel)))
			}
		}
		for rel := range fa {
			if !fb[rel] {
				res.problems = append(res.problems, fmt.Sprintf("%s: written rewritten, missing as emitted", filepath.Join(t, rel)))
				continue
			}
			rel = filepath.Join(t, rel)
			b, err := os.ReadFile(filepath.Join(before, rel))
			if err != nil {
				return res, err
			}
			a, err := os.ReadFile(filepath.Join(after, rel))
			if err != nil {
				return res, err
			}
			res.files++
			// THE SAME BYTES PREPROCESS TO THE SAME TOKENS: a file the rewrite
			// left as it was (no #else or #elif in it) has nothing to compare
			if string(a) == string(b) {
				continue
			}
			res.rewritten++
			perTree[t]++
			jobs = append(jobs, job{t, rel, lang, string(b), string(a)})
		}
	}
	for _, t := range res.trees {
		if perTree[t] == 0 {
			res.problems = append(res.problems, fmt.Sprintf("%s: the rewrite changed no file of this tree, so nothing in it was compared (is SCHEMA_CONDITIONALS_AS_EMITTED set for both sides?)", t))
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			cov, err := Compare(j.before, j.after, j.lang)
			mu.Lock()
			defer mu.Unlock()
			res.cov.Chains += cov.Chains
			res.cov.Branches += cov.Branches
			res.cov.Unreached += cov.Unreached
			if err != nil {
				res.problems = append(res.problems, j.rel+": "+err.Error())
			}
		}(j)
	}
	wg.Wait()
	sort.Strings(res.problems)
	return res, nil
}
