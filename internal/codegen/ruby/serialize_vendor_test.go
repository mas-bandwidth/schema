package ruby

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
)

// stagedSerialize is the serialize.rb tree make vendor-serialize-ruby copies
// from (docs/PORT-RUBY.md, THE BIT LAYER).
const stagedSerialize = "../../../third_party/serialize.rb/lib"

// checkSerializePin holds dir and staged to pin: every sha256 line in pin
// matches both copies of the file of that name, and no unlisted serialize .rb
// file is in dir.
func checkSerializePin(pin []byte, dir, staged string) []string {
	var bad []string
	listed := map[string]bool{}
	for line := range strings.SplitSeq(string(pin), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "sha256" {
			bad = append(bad, fmt.Sprintf("SERIALIZE_PIN: unrecognized line %q", line))
			continue
		}
		sum, name := f[1], f[2]
		listed[name] = true
		for _, where := range []string{dir, staged} {
			b, err := os.ReadFile(filepath.Join(where, filepath.FromSlash(name)))
			if err != nil {
				bad = append(bad, fmt.Sprintf("%s: %v", filepath.Join(where, name), err))
				continue
			}
			got := sha256.Sum256(b)
			if hex.EncodeToString(got[:]) != sum {
				bad = append(bad, fmt.Sprintf("%s has sha256 %s, the pin says %s", filepath.Join(where, name), hex.EncodeToString(got[:]), sum))
			}
		}
	}
	if len(listed) == 0 {
		bad = append(bad, "SERIALIZE_PIN lists no files")
	}
	sub := filepath.Join(dir, "serialize")
	if _, err := os.Stat(sub); err == nil {
		_ = filepath.WalkDir(sub, func(p string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".rb") {
				return werr
			}
			rel, rerr := filepath.Rel(dir, p)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if !listed[rel] {
				bad = append(bad, fmt.Sprintf("%s is present but the pin does not list it", rel))
			}
			return nil
		})
	}
	return bad
}

const vendoredProbe = `# frozen_string_literal: true

require_relative "serialize"

w = Serialize::WriteStream.new(8)
raise "int write" unless w.serialize_int(Serialize::Ref.new(7), 0, 7)
w.flush
puts w.data.unpack1("H*")
r = Serialize::ReadStream.new(w.data)
v = Serialize::Ref.new(0)
raise(r.refusal || "int read") unless r.serialize_int(v, 0, 7)
puts v.value

w = Serialize::WriteStream.new(8)
raise "float write" unless w.serialize_float(Serialize::Ref.new(Math::PI))
w.flush
puts w.data.unpack1("H*")
r = Serialize::ReadStream.new(w.data)
f = Serialize::Ref.new(0.0)
raise(r.refusal || "float read") unless r.serialize_float(f)
puts format("%.9g", f.value)
`

// TestVendoredRubySerializeIsThePin holds runtime/serialize.rb and
// runtime/serialize/core.rb to runtime/SERIALIZE_PIN and to the staged source
// (make vendor-serialize-ruby writes both), then loads the vendored copy under
// both pinned interpreters and round-trips one serialize_int and one
// serialize_float.
func TestVendoredRubySerializeIsThePin(t *testing.T) {
	t.Parallel()
	pin, err := os.ReadFile("runtime/SERIALIZE_PIN")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range checkSerializePin(pin, "runtime", stagedSerialize) {
		t.Error(b)
	}
	raw := RawSerializeRuntime()
	want := "07\n7\ndb0f4940\n3.14159274\n"
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{rubyrun.Run, rubyrun.RunFloor} {
		dir := t.TempDir()
		writeTree(t, dir, raw)
		if got := string(run(t, dir, "main.rb", map[string][]byte{"main.rb": []byte(vendoredProbe)})); got != want {
			t.Errorf("the vendored serialize.rb printed\n%s\nwant\n%s", got, want)
		}
	}
}

// TestVendoredRubySerializePinGoesRed is the pin's negative control: a copy of
// the vendored directory with one byte of serialize/core.rb changed, and a copy
// with an unlisted module beside it, each break the pin by name.
func TestVendoredRubySerializePinGoesRed(t *testing.T) {
	t.Parallel()
	pin, err := os.ReadFile("runtime/SERIALIZE_PIN")
	if err != nil {
		t.Fatal(err)
	}
	copyDir := func(t *testing.T) string {
		dir := t.TempDir()
		for rel, b := range RawSerializeRuntime() {
			p := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	t.Run("a changed byte", func(t *testing.T) {
		dir := copyDir(t)
		p := filepath.Join(dir, "serialize", "core.rb")
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)-2] ^= 1
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		bad := checkSerializePin(pin, dir, stagedSerialize)
		if len(bad) != 1 || !strings.Contains(bad[0], "core.rb has sha256") {
			t.Fatalf("a changed core.rb: the pin reported %q, want one breach naming core.rb", bad)
		}
	})
	t.Run("an unlisted module", func(t *testing.T) {
		dir := copyDir(t)
		p := filepath.Join(dir, "serialize", "extra.rb")
		if err := os.WriteFile(p, []byte("x = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bad := checkSerializePin(pin, dir, stagedSerialize)
		if len(bad) != 1 || !strings.Contains(bad[0], "serialize/extra.rb is present but the pin does not list it") {
			t.Fatalf("an unlisted module: the pin reported %q, want one breach naming serialize/extra.rb", bad)
		}
	})
}
