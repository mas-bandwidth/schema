package tablewire

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// These are actual old owned-corpus files, retained before the prospective
// identity migration. New pins must preserve their wire layout and every
// value byte; the announced identity change moves only header/record keys.
func TestFixedIdentityV2CorpusPreservesOldBodies(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/identity-v2/legacy-fixed-wire/*.bin")
	if err != nil || len(files) != 60 {
		t.Fatalf("retained historical files: count=%d err=%v", len(files), err)
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			old, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			current, err := os.ReadFile(filepath.Join("../../testdata/conformance/tables/wire", filepath.Base(path)))
			if err != nil {
				t.Fatal(err)
			}
			if len(old) < 20 || len(current) != len(old) || old[0] != tableFixedForm || current[0] != tableFixedForm {
				t.Fatal("file form or length changed")
			}
			layoutEnd := 20 + int(binary.LittleEndian.Uint32(old[16:20]))
			if layoutEnd > len(old) || !bytes.Equal(old[:8], current[:8]) || !bytes.Equal(old[16:layoutEnd], current[16:layoutEnd]) {
				t.Fatal("reserved header or layout changed")
			}
			root := parseFixedLayout(old[20:layoutEnd])
			if root == nil || root.size <= 0 {
				t.Fatal("historical layout is malformed")
			}
			stride := 8 + int(root.size)
			if (len(old)-layoutEnd)%stride != 0 || len(old) == layoutEnd {
				t.Fatal("historical record extent is malformed")
			}
			if bytes.Equal(old[8:16], current[8:16]) {
				t.Fatal("the prospective identity did not move")
			}
			for at := layoutEnd; at < len(old); at += stride {
				if !bytes.Equal(old[at:at+8], old[8:16]) || !bytes.Equal(current[at:at+8], current[8:16]) {
					t.Fatal("record key differs from its file key")
				}
				if !bytes.Equal(old[at+8:at+stride], current[at+8:at+stride]) {
					t.Fatal("record body changed")
				}
			}
		})
	}
}
