// AMERICAN SPELLING (issue #735, the intent of #402): the tree speaks one
// dialect. Prose, comments, goldens, tests, the Makefile and the workflows all
// use American spelling, and the identifiers we own under the compiler follow
// it, with the goldens regenerated when a renamed identifier moves one.
//
// This gate is the mechanical half of that rule: it walks the tree and refuses
// a British spelling from the sweep's own list, naming the file and the line.
// The list is the vocabulary the sweep uses; a word not on it is either a name
// we do not own (an external surface keeps its spelling) or a dialect decision
// this gate was not told about.
package compiler

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// britishSweep is the sweep's vocabulary. The -our and -is families are built
// from their stems so every inflection is covered, while the irregulars are
// listed by name; `\w*` lets the -our stems reach colouring and coloured, and
// the explicit list keeps a name like `LabelL` (Label plus a width suffix, not
// the word labelled) out of the gate.
var britishSweep = regexp.MustCompile(`(?i)\b(?:` + strings.Join(britishWords(), "|") + `)\b`)

func britishWords() []string {
	words := map[string]bool{}
	add := func(w string) { words[w] = true }
	for _, w := range []string{
		"unlabelled", "labelled", "marshalling", "licence", "theatre",
		"modelled", "modelling", "cancelled", "cancelling", "judgement",
		"analogue", "parenthesise", "parenthesised", "parenthesises",
		"parenthesising", "artefact", "artefacts", "equalling", "equalled",
		"equalise", "defence", "travelled", "travelling", "traveller",
		"travellers", "catalogue", "catalogues", "grey", "optimise",
		"minimise", "randomise", "symbolise", "generalise", "materialise",
		"initialise",
	} {
		add(w)
	}
	for _, stem := range []string{
		"normalis", "initialis", "synchronis", "localis", "materialis",
		"recognis", "memoris", "optimis", "parameteris", "symbolis",
		"capitalis", "generalis", "minimis", "randomis", "textualis",
	} {
		for _, suf := range []string{"e", "es", "ed", "ing", "ation", "ations", "er", "ers", "able", "ables"} {
			add(stem + suf)
		}
	}
	// the -our family: a prefix match reaches every inflection
	for _, w := range []string{"colour", "behaviour", "neighbour", "honour", "favour", "armour"} {
		words[w+`\w*`] = true
	}
	out := make([]string, 0, len(words))
	for w := range words {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

func TestAmericanSpelling(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(root, "compiler", "spelling_test.go")
	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "build", "serialize", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if path == self || !isTextPath(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isBinary(data) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(data), "\n") {
			if m := britishSweep.FindString(line); m != "" {
				found = append(found, rel+":"+strconv.Itoa(i+1)+": "+m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Errorf("%d British spellings survive the American sweep (issue #735):\n%s",
			len(found), strings.Join(found, "\n"))
	}
}

// isTextPath keeps the walk off the bulky binary corpora: the wire goldens
// (.bin), the cook snapshots (.cook) and images carry bytes, not a dialect.
func isTextPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bin", ".cook", ".png", ".jpg", ".jpeg", ".gif", ".ico",
		".woff", ".woff2", ".pdf", ".zip", ".gz", ".o", ".a", ".so":
		return false
	}
	return true
}

func isBinary(data []byte) bool {
	limit := len(data)
	if limit > 8192 {
		limit = 8192
	}
	for _, b := range data[:limit] {
		if b == 0 {
			return true
		}
	}
	return false
}
