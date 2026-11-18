# frozen_string_literal: true

# The roadmap page is generated: ROADMAP.md is what test/roadmap.rb makes of
# docs/roadmap.sexp, and a roadmap file of the wrong shape is refused whole.
# (make docs-check runs the examples of README.md and DESIGN.md; it starts an
# interpreter per block, so it is its own target rather than a test here.)

require 'roadmap'

class RoadmapTest < Harness::TestCase
  SOURCE = 'docs/roadmap.sexp'

  def root
    File.dirname(__dir__)
  end

  def test_roadmap_is_generated_from_the_sexp
    doc = Roadmap.decode(SOURCE, File.read(File.join(root, SOURCE), encoding: Encoding::UTF_8))
    assert(doc[:items].size.positive?, 'the roadmap holds no item')
    assert_equal(Roadmap.render(doc, SOURCE), File.read(File.join(root, 'ROADMAP.md'), encoding: Encoding::UTF_8),
                 'ROADMAP.md is not what docs/roadmap.sexp renders; run: make roadmap')
  end

  def test_a_roadmap_of_the_wrong_shape_is_refused_naming_every_problem
    text = <<~SEXP
      (roadmap "v1" :title "t" :text "x"
       :groups ((group "g" :title "G" :text "y"))
       :items ((item "a" :group "g" :title "A" :colour "red" :date "2026-11-17")
               (item "b" :group "nowhere" :title "B" :text "z" :date "17 Nov")))
    SEXP
    error = assert_raises(Roadmap::Refused) { Roadmap.decode('t.sexp', text) }
    ['unknown key :colour', 'missing key :text', ':date "17 Nov" wants YYYY-MM-DD',
     ':group "nowhere" names no (group ...) record'].each do |want|
      subtest(want) { assert(error.message.include?(want), error.message) }
    end
  end

  def test_the_file_is_data
    ['(roadmap "v1" @x)', '(roadmap "v1"', '(roadmap "v1")) ', '`(x)', '(a) (b)'].each do |text|
      subtest(text) { assert_raises(Roadmap::Refused) { Roadmap.decode('t.sexp', text) } }
    end
  end
end
