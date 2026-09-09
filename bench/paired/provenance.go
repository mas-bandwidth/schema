package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const completionFile = "completion.json"

type passFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// passManifest records integrity, not a signature: an operator who deliberately
// rewrites both the evidence and this manifest can create a different seal.
type passManifest struct {
	Version int        `json:"version"`
	Rounds  int        `json:"rounds"`
	Files   []passFile `json:"files"`
}

func passInputNames(rounds int) []string {
	names := []string{"build.json", "window.json", "load.json"}
	for _, wire := range []string{"packet", "table"} {
		for _, label := range []string{"start", "end"} {
			names = append(names, "control-"+label+"-"+wire+".csv")
		}
		for round := range rounds {
			for _, lang := range languages {
				names = append(names, fmt.Sprintf("round-%d-%s-%s.csv", round, lang, wire))
			}
		}
	}
	slices.Sort(names)
	return names
}

func readPassFile(dir, name string) ([]byte, error) {
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("pass input %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("pass input %s is not a regular file", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pass input %s: %w", name, err)
	}
	return data, nil
}

// snapshotPass permits only the raw inputs and the three reproducible render
// outputs. Rejecting the entire unexpected filename set also catches misspelled
// samples, additional rounds and unknown language or wire records.
func snapshotPass(dir string) (buildInfo, passManifest, error) {
	var info buildInfo
	var manifest passManifest
	data, err := readPassFile(dir, "build.json")
	if err != nil {
		return info, manifest, err
	}
	if err = json.Unmarshal(data, &info); err != nil {
		return info, manifest, fmt.Errorf("read pass build metadata: %w", err)
	}
	if info.Rounds < 7 {
		return info, manifest, errors.New("completed pass requires at least seven rounds")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return info, manifest, err
	}
	var actual []string
	for _, entry := range entries {
		switch entry.Name() {
		case completionFile, "README.md", "DETAILS.md", "results.csv":
			continue
		default:
			actual = append(actual, entry.Name())
		}
	}
	// Check the observed count before allocating from untrusted round metadata.
	const metadataAndControls = 7
	samplesPerRound := 2 * len(languages)
	if len(actual) < metadataAndControls || (len(actual)-metadataAndControls)%samplesPerRound != 0 || (len(actual)-metadataAndControls)/samplesPerRound != info.Rounds {
		return info, manifest, fmt.Errorf("pass file count %d does not match metadata and all samples for %d rounds", len(actual), info.Rounds)
	}
	expected := passInputNames(info.Rounds)
	if !slices.Equal(actual, expected) {
		return info, manifest, errors.New("pass contains missing or unexpected raw filenames")
	}
	manifest.Version, manifest.Rounds = 1, info.Rounds
	for _, name := range expected {
		contents, readErr := readPassFile(dir, name)
		if readErr != nil {
			return info, manifest, readErr
		}
		if name == "build.json" && !bytes.Equal(contents, data) {
			return info, manifest, errors.New("build metadata changed while reading the pass")
		}
		if name == "window.json" {
			var window windowEvidence
			if err = json.Unmarshal(contents, &window); err != nil {
				return info, manifest, fmt.Errorf("read pass window: %w", err)
			}
			if err = validatePassWindow(window, info.Rounds); err != nil {
				return info, manifest, fmt.Errorf("pass window: %w", err)
			}
		}
		hash := sha256.Sum256(contents)
		manifest.Files = append(manifest.Files, passFile{Name: name, SHA256: hex.EncodeToString(hash[:])})
	}
	return info, manifest, nil
}

func verifyRecordedArtifacts(info buildInfo) error {
	for _, group := range []struct {
		name   string
		hashes map[string]string
	}{{"binary", info.Binaries}, {"corpus", info.Corpora}} {
		if len(group.hashes) == 0 {
			return fmt.Errorf("build metadata has no recorded %s hashes", group.name)
		}
		var paths []string
		for path := range group.hashes {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		for _, path := range paths {
			actual, err := hashFile(path)
			if err != nil {
				return fmt.Errorf("verify recorded %s %s: %w", group.name, path, err)
			}
			if actual != group.hashes[path] {
				return fmt.Errorf("recorded %s changed since build: %s", group.name, path)
			}
		}
	}
	return nil
}

// sealPass is the production completion path. Relative artifact paths are
// resolved from the repository cwd, just as they were when build.json was made.
func sealPass(dir string) error {
	return sealPassWithVerifier(dir, verifyRecordedArtifacts)
}

// sealPassWithVerifier lets synthetic orchestration fixtures explicitly replace
// live artifact verification; production always uses sealPass.
func sealPassWithVerifier(dir string, verifyBuild func(buildInfo) error) error {
	path := filepath.Join(dir, completionFile)
	if _, err := os.Lstat(path); err == nil {
		return errors.New("pass already has a completion manifest")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if verifyBuild == nil {
		return errors.New("completion requires an artifact verifier")
	}
	info, manifest, err := snapshotPass(dir)
	if err != nil {
		return err
	}
	if err = verifyBuild(info); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".completion-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	_, writeErr := temp.Write(append(data, '\n'))
	closeErr := temp.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// verifyPass verifies archived evidence only. Completed artifacts can be moved
// to another host or rendered after the original build outputs have been removed.
func verifyPass(dir string) error {
	data, err := readPassFile(dir, completionFile)
	if err != nil {
		return fmt.Errorf("unsealed pass: %w", err)
	}
	var recorded passManifest
	if err = json.Unmarshal(data, &recorded); err != nil {
		return fmt.Errorf("read completion manifest: %w", err)
	}
	if recorded.Version != 1 {
		return fmt.Errorf("unsupported completion manifest version %d", recorded.Version)
	}
	_, actual, err := snapshotPass(dir)
	if err != nil {
		return err
	}
	if recorded.Rounds != actual.Rounds || len(recorded.Files) != len(actual.Files) {
		return errors.New("completion manifest round or file count differs from the pass")
	}
	for i, file := range actual.Files {
		if file.Name != recorded.Files[i].Name {
			return errors.New("completion manifest does not contain the exact expected file list")
		}
		if file.SHA256 != recorded.Files[i].SHA256 {
			return fmt.Errorf("sealed pass input changed: %s", file.Name)
		}
	}
	return nil
}
