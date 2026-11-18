# frozen_string_literal: true

# The vendored family files are the pin VENDORED names, byte for byte.
#
# STANDARD.md, IMPLEMENTING.md, conformance/ and conformance-controls/ come
# from mas-bandwidth/serialize at one commit. Every file VENDORED lists has its
# sha256, and no file is present under those roots that the list does not
# name, so a corpus edited in place, or a vector added beside it, is red here
# before the runner reads it.

require 'digest'

module Vendored
  ROOT = File.dirname(__dir__)
  DIRECTORIES = %w[conformance conformance-controls].freeze
  FILES = %w[STANDARD.md IMPLEMENTING.md].freeze

  # [header fields, path => sha256] of a VENDORED file's text
  def self.read_pin(text)
    fields = {}
    sums = {}
    text.each_line do |line|
      next if line.strip.empty? || line.start_with?('#')

      words = line.split
      if words[0] == 'sha256'
        malformed = words.size != 3 || sums.key?(words[2])
        raise ArgumentError, "VENDORED: malformed or repeated line #{line.inspect}" if malformed

        sums[words[2]] = words[1]
      elsif words.size == 2 && %w[repository tag commit].include?(words[0])
        fields[words[0]] = words[1]
      else
        raise ArgumentError, "VENDORED: unrecognized line #{line.inspect}"
      end
    end
    [fields, sums]
  end

  def self.problems(root, sums)
    present = FILES.select { |f| File.exist?(File.join(root, f)) }
    DIRECTORIES.each do |d|
      dir = File.join(root, d)
      present.concat(Dir.children(dir).map { |f| "#{d}/#{f}" }) if File.directory?(dir)
    end
    out = []
    sums.sort.each do |path, want|
      unless present.include?(path)
        out << "#{path} is missing"
        next
      end
      got = Digest::SHA256.file(File.join(root, path)).hexdigest
      out << "#{path} has sha256 #{got}, the pin is #{want}" if got != want
    end
    out + (present - sums.keys).sort.map { |path| "#{path} is present and the pin does not list it" }
  end

  def self.pin
    read_pin(File.read(File.join(ROOT, 'VENDORED'), encoding: 'UTF-8'))
  end
end

class VendoredTest < Harness::TestCase
  def test_the_vendored_files_are_the_pin
    fields, sums = Vendored.pin
    assert_equal('https://github.com/mas-bandwidth/serialize', fields['repository'])
    assert_equal(40, fields.fetch('commit', '').size)
    assert(fields.fetch('tag', '').start_with?('v'))
    Vendored::FILES.each { |name| assert(sums.key?(name), "#{name} is not pinned") }
    assert_operator(sums.keys.count { |p| p.start_with?('conformance/') }, :>, 0)
    assert_equal([], Vendored.problems(Vendored::ROOT, sums))
  end

  def test_a_changed_or_unlisted_file_is_refused
    # the check's reversed witness, over the real tree: a wrong sum and a list
    # missing one file must each be named
    _, sums = Vendored.pin
    wrong = sums.merge('STANDARD.md' => '0' * 64)
    assert(Vendored.problems(Vendored::ROOT, wrong).any? { |p| p.include?('STANDARD.md has sha256') })
    short = sums.except('conformance/int.txt')
    named = 'conformance/int.txt is present and the pin does not list it'
    assert(Vendored.problems(Vendored::ROOT, short).include?(named))
  end
end
