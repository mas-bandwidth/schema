package compiler_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestRubyFixedWriterLock(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	runRubyFixedWriterLock(t, false)
}

func TestRubyFixedWriterLockNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Ruby interpreters")
	runRubyFixedWriterLock(t, true)
}

func runRubyFixedWriterLock(t *testing.T, sabotage bool) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	write := func(src string) {
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := compiler.New()
	write("package probe\nfixed table T { x int32 | min = -5, max = 5\n s int128 | min = -500, max = 500 }\n")
	oldUnit, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.UpdateSchemaLock(oldUnit, []string{path}); err != nil {
		t.Fatal(err)
	}
	oldFiles, err := c.Generate(oldUnit, "ruby", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	write("package probe\nfixed table T { x int32 | min = -10, max = 10\n s int128 | min = -1000, max = 1000 }\n")
	newUnit, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := compiler.UpdateSchemaLock(newUnit, []string{path}); err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(newUnit, "ruby", compiler.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sabotage {
		name := "probe/_fixed_data.rb"
		source := string(files[name])
		anchor := `"hi" => 5,`
		if strings.Count(source, anchor) != 1 {
			t.Fatalf("writer range anchor count = %d", strings.Count(source, anchor))
		}
		files[name] = []byte(strings.Replace(source, anchor, `"hi" => 10,`, 1))
	}
	for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
		olddir := t.TempDir()
		rubyWriterTree(t, olddir, oldFiles)
		script := `# frozen_string_literal: true
require_relative "probe"
v = Probe::T.new
v.x = 5
v.s = 500
File.binwrite("oldwire", v.save)
`
		if _, stderr, rerr := rubyrun.Try(t, pin, olddir, "main.rb", map[string][]byte{"main.rb": []byte(script)}); rerr != nil || stderr != "" {
			t.Fatalf("old writer: %v\n%s", rerr, stderr)
		}
		wire, rerr := os.ReadFile(filepath.Join(olddir, "oldwire"))
		if rerr != nil {
			t.Fatal(rerr)
		}
		newdir := t.TempDir()
		rubyWriterTree(t, newdir, files)
		script = `# frozen_string_literal: true
require_relative "probe"
wire = File.binread("oldwire")
v = Probe::T.load(wire)
raise "mismatch" unless v.x == 5 && v.s == 500
start = 20 + wire.byteslice(16, 4).unpack1("L<") + 8
[[0, 4, 6], [0, 4, -6], [4, 16, 501], [4, 16, -501]].each do |offset, size, val|
  bad = wire.b
  if size == 4
    bad[start + offset, 4] = [val & 0xFFFFFFFF].pack("L<")
  else
    bad[start + offset, 16] = [val & 0xFFFFFFFFFFFFFFFF, (val >> 64) & 0xFFFFFFFFFFFFFFFF].pack("Q<Q<")
  end
  report = Probe::FixedLoadReport.new
  report.layout_hash = 123
  report.widened = 456
  begin
    Probe::T.load(bad, report)
  rescue Probe::Refused
    # expected
  else
    abort("historical writer bounds lost")
  end
  raise "report mutated" unless report.layout_hash == 123 && report.widened == 456
end
`
		_, stderr, rerr := rubyrun.Try(t, pin, newdir, "main.rb", map[string][]byte{"main.rb": []byte(script), "oldwire": wire})
		if sabotage {
			if rerr == nil || !strings.Contains(stderr, "historical writer bounds lost") {
				t.Fatalf("writer-bound sabotage did not fail by name: %v\n%s", rerr, stderr)
			}
		} else if rerr != nil || stderr != "" {
			t.Fatalf("locked reader: %v\n%s", rerr, stderr)
		}
	}
}

func rubyWriterTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
