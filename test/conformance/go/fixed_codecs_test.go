package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestFixedCodecFuzzForm3(t *testing.T) {
	wire, err := os.ReadFile("../../../testdata/conformance/tables/wire/root_default.bin")
	if err != nil {
		t.Fatal(err)
	}
	var request bytes.Buffer
	put := func(v any) { _ = binary.Write(&request, binary.LittleEndian, v) }
	put(uint32(1))
	for _, name := range []string{"tabledemo", "RootConfig"} {
		put(uint16(len(name)))
		request.WriteString(name)
	}
	request.Write([]byte{3, 0}) // actual form, no retention
	put(uint32(0))
	put(uint32(len(wire)))
	request.Write(wire)
	dir := t.TempDir()
	in, err := os.Create(filepath.Join(dir, "in"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Write(request.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	stdin, stdout := os.Stdin, os.Stdout
	runErr := func() error {
		os.Stdin, os.Stdout = in, out
		defer func() { os.Stdin, os.Stdout = stdin, stdout }()
		return wireFuzz(false)
	}()
	if runErr != nil {
		t.Fatal(runErr)
	}
	answer, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	const header = 47
	if len(answer) < 1+header || answer[0] != 1 {
		t.Fatalf("fixed form absent from fuzz roster: %x", answer)
	}
	head := answer[1 : 1+header]
	if head[0] != 1 || head[21] != 0 || head[22] != 0 || int64(binary.LittleEndian.Uint64(head[39:])) != int64(len(wire)) || !bytes.Equal(answer[1+header:], wire) {
		t.Fatal("native fixed fuzz read did not return the complete saved fixture")
	}
}

// The committed form-3 fixtures are independent engine writes. Every fixed
// instance must travel through the real driver callbacks in both directions,
// with its complete JSON value preserved. No expected answer enters a driver.
func TestFixedCodecMigratedInstances(t *testing.T) {
	const repo = "../../.."
	lines, err := readManifest(filepath.Join(repo, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range lines {
		if row[0] != "instance" {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(repo, row[4]))
		if err != nil {
			t.Fatal(err)
		}
		if len(wire) == 0 || wire[0] != 3 {
			continue
		}
		count++
		t.Run(row[1], func(t *testing.T) {
			c := findCodec(row[2], row[3])
			if c == nil {
				t.Fatalf("no native fixed driver row for %s.%s", row[2], row[3])
			}
			value := c.fresh()
			var rep report
			if !c.load(value, wire, &rep) || rep.refused {
				t.Fatal("native fixed fixture refused")
			}
			save := func(value any) []byte {
				t.Helper()
				n := c.measure(value)
				if n < 0 {
					t.Fatalf("fixed measure refused: %d", n)
				}
				out := make([]byte, n)
				written := c.save(value, out)
				if written < 0 || written > int64(len(out)) {
					t.Fatalf("fixed save refused or exceeded its measure: %d", written)
				}
				return out[:written]
			}
			if out := save(value); !bytes.Equal(out, wire) {
				t.Fatalf("native fixed write-back differs: %d bytes, want %d", len(out), len(wire))
			}
			if !noText(row) {
				text, err := os.ReadFile(filepath.Join(repo, "testdata/conformance/tables/json", row[1]+".json"))
				if err != nil {
					t.Fatal(err)
				}
				n := c.toJsonMeasure(value)
				if n < 0 {
					t.Fatal("accepted fixed value has no JSON measure")
				}
				out := make([]byte, n)
				written := c.toJson(value, out)
				if written < 0 || written > int64(len(out)) || !bytes.Equal(out[:written], text) {
					t.Fatal("native fixed value differs from the complete JSON fixture")
				}
				fromText := c.fresh()
				if !c.fromJson(fromText, text, &rep) || rep.refused {
					t.Fatal("fixed JSON fixture refused")
				}
				if out := save(fromText); !bytes.Equal(out, wire) {
					t.Fatalf("JSON read did not write the native fixed bytes: %d bytes, want %d", len(out), len(wire))
				}
			}
			// A fixed root must refuse a real, minimal form-1 file. An old
			// variable-form callback would admit its empty body at defaults.
			previous := make([]byte, 10)
			previous[0] = 1
			if c.load(c.fresh(), previous, &rep) || !rep.refused {
				t.Fatal("fixed driver admitted the previous form")
			}
		})
	}
	if count == 0 {
		t.Fatal("no migrated fixed fixtures exercised")
	}
	t.Logf("%d native fixed fixture roundtrips and full JSON values checked", count)
}
