package rubytable_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRubyFixedVersioningWriterRangeHostile: THE WRITER'S RANGE IS THE BOUND
// (docs/FIXED-FORM-ALGORITHM.md §4.6; docs/rulings.json strict-only-read, which
// overturned schema#1164 statement B). The shared cross-version vectors every
// leg reads (fixedvector.WriterRange), from the C++ reference's corpus: an OLD
// writer's file with one ranged value forged past that writer's range, read by
// the NEW reader whose own range is wider — an integer scalar, one element of a
// ranged fixed array, a uint64 at or above 2^63, and a compressed float
// compared in its own type (the float32 value against the float32-rounded
// bound). Each refuses the whole load out_of_range, as the C++ reference's NEW
// reader does. The controls: each row's unforged old file lands (the compressed
// float's at 0.5 exactly, its writer's range narrower than the reader's), and
// with the emitted float range check planted out the forged compressed float
// lands, so the refusal is that check's.
func TestRubyFixedVersioningWriterRangeHostile(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Ruby interpreters and C++ fixed-form corpus")
	repo := filepath.Join("..", "..", "..")
	corpus := filepath.Join(repo, "build", "fixedform-corpus")
	rows := map[string][]fixedvector.Vector{}
	var order []string
	for _, vec := range fixedvector.WriterRange {
		if rows[vec.Row] == nil {
			order = append(order, vec.Row)
		}
		rows[vec.Row] = append(rows[vec.Row], vec)
	}
	good := map[string]string{
		"range_widen":        `abort("writer_range_good") if rows.length != 1`,
		"cfloat_range_widen": `abort("writer_range_good") if rows.length != 1 || rows[0].lead != 1 || rows[0].aim != 0.5 || rows[0].trail != 2`,
	}
	for _, row := range order {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			vecs := rows[row]
			old := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_"+row+".schema")
			newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_"+row+".schema")
			root := vecs[0].Root
			entry, ok := fixedproof.LineageEntryOf(old, root)
			if !ok {
				t.Fatal("missing writer layout")
			}
			files, err := rubytable.GenerateLineage(newer, map[string][]rubytable.FixedLineageEntry{root: {{Wire: entry.Wire, Layout: entry.Layout, Record: entry.Record, Digest: entry.Digest, Writer: old.Tables[root]}}})
			if err != nil {
				t.Fatal(err)
			}
			inputs := map[string][]byte{}
			names := []string{}
			for _, vec := range vecs {
				want, err := fixedvector.Read(corpus, vec.File)
				if err != nil {
					t.Fatal(err)
				}
				if !want.Refused {
					t.Fatalf("the C++ reference landed %s: %+v", vec.File, want)
				}
				names = append(names, vec.File)
			}
			for _, name := range append([]string{"old_" + row + ".bin"}, names...) {
				if inputs[name], err = os.ReadFile(filepath.Join(corpus, name)); err != nil {
					t.Fatalf("C++ reference corpus %s: %v", name, err)
				}
			}
			check, ok := good[row]
			if !ok {
				t.Fatalf("no good-file check for row %s", row)
			}
			list := make([]string, len(names))
			for i, n := range names {
				list[i] = fmt.Sprintf("%q", n)
			}
			script := fmt.Sprintf(`require_relative %q
rows = %s::%s.fixed_load(File.binread(%q))
%s
[%s].each do |name|
  begin
    %s::%s.fixed_load(File.binread(name))
  rescue %s::Refused => e
    abort("writer_range_reason_#{name}_#{e.reason}") unless e.reason == "out_of_range"
  else
    abort("writer_range_control_#{name}")
  end
end
puts "writer-range"
`, newer.Package, ruby.ModuleName(newer.Package), root, "old_"+row+".bin", check, strings.Join(list, ", "), ruby.ModuleName(newer.Package), root, ruby.ModuleName(newer.Package))
			if out, _ := rubyFixedRun(t, files, script, inputs); strings.TrimSpace(out) != "writer-range" {
				t.Fatalf("writer range: %s", out)
			}
			if row != "cfloat_range_widen" {
				return
			}
			// the negative control: the float range check planted out, the
			// forged compressed float lands
			path := newer.Package + "/_fixed.rb"
			source := string(files[path])
			anchor := "if spec[\"ranged\"] && (fval < spec[\"lo\"] || fval > spec[\"hi\"])\n        raise Refused.new(\"out_of_range\")"
			if strings.Count(source, anchor) != 1 {
				t.Fatalf("float range check anchor count %d in %s", strings.Count(source, anchor), path)
			}
			sabotaged := maps.Clone(files)
			sabotaged[path] = []byte(strings.Replace(source, anchor, "if false\n        raise Refused.new(\"out_of_range\")", 1))
			for _, pin := range []string{rubyrun.Interpreter(t), rubyrun.Floor(t)} {
				dir := rubyFixedTree(t, sabotaged)
				in := maps.Clone(inputs)
				in["main.rb"] = []byte("# frozen_string_literal: true\n" + script)
				_, stderr, rerr := rubyrun.Try(t, pin, dir, "main.rb", in)
				if rerr == nil || !strings.Contains(stderr, "writer_range_control_hostile_cfloat_range_widen.bin") {
					t.Fatalf("float range check sabotage escaped: %v\n%s", rerr, stderr)
				}
			}
		})
	}
}
