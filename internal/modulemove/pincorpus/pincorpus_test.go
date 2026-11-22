package pincorpus

import (
	"slices"
	"strings"
	"testing"
)

// TestPinCorpusEveryFileIsRead holds the folder to the loaders: every file
// under testdata/pin is a case file of one of the three kinds and is read by
// that kind's loader (the set of files the cases came from is the set the
// folder holds), no kind is empty, and each kind carries both a case that
// must be refused and a control that must pass. A file dropped beside the
// corpus that nothing reads fails here, and so does an emptied folder.
func TestPinCorpusEveryFileIsRead(t *testing.T) {
	t.Parallel()
	onDisk, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk) == 0 {
		t.Fatalf("%s holds no file", Root)
	}
	var read []string
	note := func(file string) {
		if !slices.Contains(read, file) {
			read = append(read, file)
		}
	}
	verdicts := map[string]map[bool]int{}
	count := func(kind string, passes bool) {
		if verdicts[kind] == nil {
			verdicts[kind] = map[bool]int{}
		}
		verdicts[kind][passes]++
	}

	spellings, err := Spellings()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range spellings {
		note(c.File)
		count("versions", c.Verdict == "accept")
	}
	trees, err := Trees()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range trees {
		note(c.File)
		count("trees", c.Error == "")
	}
	releases, err := Releases()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range releases {
		note(c.File)
		count("releases", c.Verdict == "accept")
	}

	slices.Sort(read)
	if !slices.Equal(read, onDisk) {
		t.Errorf("the folder holds %d files and the loaders read %d:\nfolder: %s\nread:   %s", len(onDisk), len(read), strings.Join(onDisk, " "), strings.Join(read, " "))
	}
	for _, kind := range Kinds {
		if verdicts[kind][true] == 0 || verdicts[kind][false] == 0 {
			t.Errorf("%s/%s holds %d passing and %d refused cases; want both: a case beside its control", Root, kind, verdicts[kind][true], verdicts[kind][false])
		}
	}
	t.Logf("pin corpus: %d files, %d spellings, %d tree cases, %d release cases", len(onDisk), len(spellings), len(trees), len(releases))
}
