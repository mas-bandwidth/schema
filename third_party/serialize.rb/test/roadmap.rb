# frozen_string_literal: true

# Renders ROADMAP.md from docs/roadmap.sexp, so the page is never written by
# hand. Offline and standard library only. The file is data: it is read as an
# s-expression (lists, strings, :keywords, symbols, integers, ; comments) and
# nothing in it is evaluated.
#
#   usage: <ruby> -w test/roadmap.rb --file docs/roadmap.sexp --out ROADMAP.md
#
# The shape is the family's roadmap form:
#
#   (roadmap "v1"
#    :title "<page title>"
#    :text "<the page's opening paragraph>"
#    :groups ((group "<id>" :title "..." :text "...") ...)
#    :items ((item "<id>" :group "<group id>" :title "..." :text "..." :date "YYYY-MM-DD"
#             [:why "..."] [:exists "..."] [:origin "..."] [:release "..."]) ...))
#
# A file not exactly that shape is refused whole, every problem named.
# test/test_docs.rb holds the committed ROADMAP.md to what this writes.
module Roadmap
  # a file that is not a roadmap, with every problem found
  class Refused < StandardError; end

  FORMAT = 'v1'
  MAX_BYTES = 1 << 20

  # one atom or list of the file: kind is :list, :string, :keyword, :symbol or :integer
  Form = Struct.new(:kind, :value, :offset)

  TOKEN = /\G(?:(?<space>\s+|;[^\n]*)|(?<open>\()|(?<close>\))|"(?<string>(?:[^"\\]|\\.)*)"|
            :(?<keyword>[a-z][a-z0-9-]*)|(?<integer>-?\d+)(?![^\s()])|(?<symbol>[a-z][a-z0-9-]*))/mx

  module_function

  # the one top form of text, or Refused
  def read(file, text)
    if text.bytesize > MAX_BYTES
      raise Refused, "roadmap: file=#{file}: #{text.bytesize} bytes, the bound is #{MAX_BYTES}"
    end

    stack = [[]]
    pos = 0
    while pos < text.bytesize
      m = TOKEN.match(text, pos)
      raise Refused, "roadmap: file=#{file}: byte=#{pos}: not a list, string, keyword, symbol or integer" unless m

      if m[:open]
        stack << []
      elsif m[:close]
        raise Refused, "roadmap: file=#{file}: byte=#{pos}: a ) closes nothing" if stack.size == 1

        list = stack.pop
        stack.last << Form.new(:list, list, pos)
      elsif m[:string]
        stack.last << Form.new(:string, m[:string].gsub(/\\(.)/m, '\1'), pos)
      elsif m[:keyword]
        stack.last << Form.new(:keyword, m[:keyword], pos)
      elsif m[:integer]
        stack.last << Form.new(:integer, Integer(m[:integer], 10), pos)
      elsif m[:symbol]
        stack.last << Form.new(:symbol, m[:symbol], pos)
      end
      pos = m.end(0)
    end
    raise Refused, "roadmap: file=#{file}: a ( is never closed" if stack.size > 1
    raise Refused, "roadmap: file=#{file}: want one top form, found #{stack[0].size}" unless stack[0].size == 1

    stack[0][0]
  end

  # Decodes one record form and its keys.
  class Decoder
    attr_reader :problems

    def initialize(file)
      @file = file
      @problems = []
    end

    def problem(at, why)
      @problems << "roadmap: file=#{@file} #{at}: #{why}"
      nil
    end

    # [id, {key => Form}] of (head "<id>" :key value ...), or nil
    def record(at, form, head, required, optional)
      unless form.kind == :list && form.value[0]&.kind == :symbol && form.value[0].value == head
        return problem(at, "want a (#{head} ...) record")
      end

      rest = form.value.drop(1)
      unless rest[0]&.kind == :string && !rest[0].value.strip.empty?
        return problem(at, "(#{head}) wants its identity, a non-empty string, after the head")
      end

      id = rest.shift.value
      at = "#{at} #{head}=#{id}"
      values = {}
      rest.each_slice(2) do |key, value|
        return problem(at, "want a :key at byte=#{key.offset}") unless key.kind == :keyword
        return problem(at, ":#{key.value} has no value") if value.nil?

        if !(required + optional).include?(key.value)
          problem(at, "unknown key :#{key.value}")
        elsif values.key?(key.value)
          problem(at, "key :#{key.value} stands twice")
        else
          values[key.value] = value
        end
      end
      (required - values.keys).each { |k| problem(at, "missing key :#{k}") }
      [id, values, at]
    end

    def str(at, values, key)
      form = values[key]
      return nil if form.nil?
      return form.value if form.kind == :string && !form.value.strip.empty?

      problem(at, ":#{key} wants a non-empty string")
    end

    def list(at, values, key)
      form = values[key]
      return [] if form.nil?
      return form.value if form.kind == :list

      problem(at, ":#{key} wants a list") || []
    end

    def date(at, values)
      v = str(at, values, 'date')
      problem(at, ":date #{v.inspect} wants YYYY-MM-DD") if v && !/\A\d{4}-\d{2}-\d{2}\z/.match?(v)
      v
    end
  end

  ITEM_OPTIONAL = %w[why exists origin release].freeze

  # {title:, text:, groups: [{id:, title:, text:}], items: [{id:, group:, ...}]}, or Refused
  def decode(file, text)
    top = read(file, text)
    d = Decoder.new(file)
    unless top.kind == :list && top.value[1]&.kind == :string && top.value[1].value == FORMAT
      raise Refused, "roadmap: file=#{file} top: want one (roadmap #{FORMAT.inspect} ...) form"
    end

    _, values, = d.record('top', top, 'roadmap', %w[title text groups items], [])
    raise Refused, d.problems.join('; ') if values.nil? || !d.problems.empty?

    doc = { title: d.str('top', values, 'title'), text: d.str('top', values, 'text'), groups: [], items: [] }
    ids = {}
    seen = lambda do |at, kind, id|
      d.problem(at, "id #{id.inspect} is already used by a (#{ids[id]} ...) record") if ids.key?(id)
      ids[id] = kind
    end
    d.list('top', values, 'groups').each_with_index do |f, i|
      id, v, at = d.record(":groups[#{i}]", f, 'group', %w[title text], [])
      next if v.nil?

      seen.call(at, 'group', id)
      doc[:groups] << { id: id, title: d.str(at, v, 'title'), text: d.str(at, v, 'text') }
    end
    d.list('top', values, 'items').each_with_index do |f, i|
      id, v, at = d.record(":items[#{i}]", f, 'item', %w[group title text date], ITEM_OPTIONAL)
      next if v.nil?

      seen.call(at, 'item', id)
      item = { id: id, group: d.str(at, v, 'group'), title: d.str(at, v, 'title'), text: d.str(at, v, 'text'),
               date: d.date(at, v) }
      ITEM_OPTIONAL.each { |k| item[k.to_sym] = d.str(at, v, k) }
      d.problem(at, ":group #{item[:group].inspect} names no (group ...) record") unless ids[item[:group]] == 'group'
      doc[:items] << item
    end
    doc[:groups].each do |g|
      next if doc[:items].any? { |item| item[:group] == g[:id] }

      d.problem(':groups', "group #{g[:id].inspect} holds no item")
    end
    raise Refused, d.problems.join('; ') unless d.problems.empty?

    doc
  end

  # every run of whitespace joined into one blank
  def flat(text)
    text.split.join(' ')
  end

  # GitHub's heading anchor: lower case, punctuation dropped, blanks to hyphens
  def anchor(title)
    flat(title).downcase.gsub(/[^a-z0-9 -]/, '').tr(' ', '-')
  end

  # the page the document makes: the same bytes for the same document
  def render(doc, source)
    out = "# #{flat(doc[:title])}\n\n"
    out << "<!-- Generated from #{source} by test/roadmap.rb. Do not edit this file: " \
           "edit #{source} and run `make roadmap`. -->\n\n"
    out << "#{flat(doc[:text])}\n"
    out << "\n## Contents\n\n"
    out << doc[:groups].map do |g|
      "- [#{flat(g[:title])}](##{anchor(g[:title])}) (#{doc[:items].count { |item| item[:group] == g[:id] }})\n"
    end.join
    doc[:groups].each do |g|
      out << "\n## #{flat(g[:title])}\n\n#{flat(g[:text])}\n"
      doc[:items].each do |item|
        next unless item[:group] == g[:id]

        out << "\n### #{flat(item[:title])}\n\n#{flat(item[:text])}\n"
        out << "\nAlready in the code: #{flat(item[:exists])}\n" if item[:exists]
        out << "\nWhy it waits: #{flat(item[:why])}\n" if item[:why]
        out << "\nTarget: #{flat(item[:release])}\n" if item[:release]
        out << "\nFrom: #{flat(item[:origin])}\n" if item[:origin]
      end
    end
    out
  end

  def main(args)
    opts = args.each_slice(2).to_h
    file = opts.delete('--file')
    out = opts.delete('--out')
    unless file && out && opts.empty? && args.size.even?
      warn 'roadmap.rb REFUSES: want --file <roadmap.sexp> --out <ROADMAP.md> and nothing else'
      return 2
    end
    doc = decode(file, File.read(file, encoding: Encoding::UTF_8))
    File.write(out, render(doc, file))
    puts "roadmap OK items=#{doc[:items].size} file=#{out}"
    0
  rescue Refused, SystemCallError => e
    warn "roadmap.rb REFUSED: #{e.message}; run: make roadmap"
    2
  end
end

exit(Roadmap.main(ARGV)) if $PROGRAM_NAME == __FILE__
