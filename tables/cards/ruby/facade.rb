# frozen_string_literal: true

# Owning card facade over the generated Card table (docs/CARDS.md; docs/PORT-RUBY.md).
# Reads keep their original wire, edits publish only after validation, and
# customer limits apply before any replacement is written.

require 'digest'
require 'cards'

module CardsFacade
  MAX_BODY_BYTES = 65_536
  MAX_LIST_ENTRIES = 512
  MAX_RETAIN_BYTES = 1 << 20
  MAX_RETAIN_IDS = 4096

  TIERS = { 'flash' => 1, 'pro' => 2, 'heavy' => 3, 'frontier' => 4 }.freeze
  PRIORITIES = { 'normal' => 1, 'high' => 2, 'critical' => 3, 'blocker' => 4 }.freeze
  COLUMNS = {
    'waiting' => 1, 'ready' => 2, 'working' => 3, 'review' => 4,
    'merging' => 5, 'landed' => 6, 'plan' => 7, 'check' => 8
  }.freeze
  RESERVED = %w[
    RESULT tier REPO BASE KIND PRIORITY PATHS NEW SHARED DEPENDS-ON TEST DONE-WHEN DEADLINE SOURCE
  ].to_h { |k| [k, true] }.freeze
  STRANDED = %w[
    KIND PATHS TEST LEGS SOURCE START STOP DEPENDS-ON PRIORITY NEW SHARED REPO BASE tier RESULT
  ].to_h { |k| [k, true] }.freeze
  HEADER_RE = /\A([A-Za-z][A-Za-z0-9-]*):[ \t\f\r]*(.*)\z/m
  KEY_RE = /\A[A-Za-z][A-Za-z0-9-]*\z/
  SHA_RE = /\A(?:[0-9a-fA-F]{12}|[0-9a-fA-F]{40})\z/
  DEADLINE_RE = /\Afinish within ([0-9]+) minutes\.\z/

  class CardError < StandardError
    attr_reader :reason

    def initialize(reason)
      super(reason)
      @reason = reason
    end
  end

  class Record
    attr_accessor :wire, :root, :report

    def initialize(wire, root, report)
      @wire = wire
      @root = root
      @report = report
    end
  end

  def self.check_text!(value, bound = nil)
    raise CardError, 'malformed' unless value.is_a?(String)
    raise CardError, 'malformed' unless [Encoding::UTF_8, Encoding::US_ASCII].include?(value.encoding)
    raise CardError, 'malformed' unless value.valid_encoding?
    raise CardError, 'malformed' if value.include?("\x00") || (!bound.nil? && value.bytesize > bound)
  end

  def self.blob_text(value)
    payload = value.is_a?(Cards::BlobRef) ? value.value : value
    raise CardError, 'malformed' if !payload.nil? && !payload.is_a?(String)

    payload
  end

  def self.check_card!(root)
    body = blob_text(root.body)
    unless body.nil?
      check_text!(body)
      raise CardError, 'body-bound 65536' if body.bytesize > MAX_BODY_BYTES
    end
    %w[paths _new shared needs extra consumers].each do |name|
      seq = root.public_send(name.to_sym)
      raise CardError, "list-bound #{name} 512" if seq.length > MAX_LIST_ENTRIES
    end
    %w[paths _new shared needs].each do |name|
      bound = name == 'needs' ? 64 : 192
      root.public_send(name.to_sym).each { |item| check_text!(item.s, bound) }
    end
    root.extra.each do |item|
      check_text!(item.key, 32)
      raise CardError, 'malformed' if !item.key.match?(KEY_RE) || RESERVED[item.key]

      val = blob_text(item.value)
      next if val.nil?

      check_text!(val)
      raise CardError, 'malformed' if val.include?("\n") || val.include?("\r")
    end
    root.consumers.each do |item|
      %w[kind who route model outcome line].each do |fname|
        check_text!(item.public_send(fname.to_sym))
      end
    end
  end

  def self.save_card(root)
    check_card!(root)
    root.save
  rescue Cards::InvalidValue, TypeError, ArgumentError
    raise CardError, 'malformed'
  end

  def self.load(wire)
    report = Cards::LoadReport.new
    raw = wire.dup.force_encoding(Encoding::BINARY)
    root = Cards::Card.load(raw, report)
    check_card!(root)
    Record.new(raw, root, report)
  rescue Cards::Refused
    raise CardError, 'malformed'
  end

  def self.read_strict(wire)
    rec = load(wire)
    raise CardError, 'unknown-present' if rec.report.unknown.positive? || rec.report.retain_lost.positive?

    rec
  end

  def self.from_json(text)
    check_text!(text)
    report = Cards::LoadReport.new
    root = Cards::Card.from_json(text, report)
    raise CardError, 'malformed' if report.unknown.positive?

    Record.new(save_card(root), root, report)
  rescue Cards::Refused, Cards::InvalidValue
    raise CardError, 'malformed'
  end

  def self.to_json(record)
    record.root.to_json
  rescue Cards::Refused, Cards::InvalidValue
    raise CardError, 'malformed'
  end

  def self.build_row(cls, value)
    obj = cls.new
    obj.s = value
    obj
  end

  def self.line_content(line)
    line = line[0...-1] if line.end_with?("\n")
    line = line[0...-1] if line.end_with?("\r")
    line
  end

  def self.parse(text)
    check_text!(text)
    parts = text.split("\n", -1)
    lines = parts[0...-1].map { |p| "#{p}\n" }
    lines << parts[-1] if parts[-1] && !parts[-1].empty?
    raise CardError, 'malformed' if lines.empty?

    first = HEADER_RE.match(line_content(lines[0]))
    raise CardError, 'malformed' if first.nil? || first[1] != 'RESULT'

    val = first[2] || ''
    tier_idx = val.rindex(' tier:')
    raise CardError, 'malformed' if tier_idx.nil?

    rest = val[0...tier_idx] || ''
    tier = (val[(tier_idx + 6)..] || '').strip
    sha_idx = rest.rindex(' sha=')
    raise CardError, 'malformed' if sha_idx.nil?

    ident = (rest[0...sha_idx] || '').strip
    sha = rest[(sha_idx + 5)..] || ''
    check_text!(ident, 64)
    raise CardError, 'malformed' if !sha.match?(SHA_RE) || !TIERS.key?(tier)

    root = Cards::Card.new
    root.id = ident
    root.sha = sha
    root.tier = TIERS[tier]
    seen = { 'RESULT' => true }
    body_at = lines[0].length
    offset = body_at
    prose = false
    strings = { 'REPO' => ['repo', 96], 'BASE' => ['_base', 96], 'KIND' => ['kind', 24], 'TEST' => ['test', 192] }
    lists = {
      'PATHS' => ['paths', Cards::PathText, 192],
      'NEW' => ['_new', Cards::PathText, 192],
      'SHARED' => ['shared', Cards::PathText, 192],
      'DEPENDS-ON' => ['needs', Cards::CardID, 64]
    }
    lines[1..].each do |line|
      content = line_content(line)
      line_end = offset + line.length
      offset = line_end
      next if content.gsub(/[ \t\r]/, '').empty?

      hdr = HEADER_RE.match(content)
      if hdr.nil?
        prose = true
        next
      end
      key = hdr[1] || ''
      hval = hdr[2] || ''
      if prose
        raise CardError, "stranded-key #{key}" if STRANDED[key]

        next
      end
      raise CardError, 'malformed' if hval.include?("\r") || hval.include?("\n")

      if RESERVED[key]
        raise CardError, 'malformed' if seen[key]

        seen[key] = true
      end
      if key == 'tier'
        raise CardError, 'malformed' if hval.strip != tier
      elsif strings.key?(key)
        fname, bound = strings[key]
        check_text!(hval, bound)
        root.public_send(:"#{fname}=", hval)
      elsif key == 'PRIORITY'
        stripped = hval.strip
        raise CardError, 'malformed' unless PRIORITIES.key?(stripped)

        root.priority = PRIORITIES[stripped]
      elsif lists.key?(key)
        fname, cls, bound = lists[key]
        stripped = hval.strip
        items = if ['', '-'].include?(stripped) || (key == 'PATHS' && stripped == 'none')
                  []
                else
                  stripped.split(',').map(&:strip).reject(&:empty?)
                end
        raise CardError, 'malformed' if items.length > MAX_LIST_ENTRIES

        items.each { |item| check_text!(item, bound) }
        root.public_send(:"#{fname}=", items.map { |item| build_row(cls, item) })
      elsif key == 'DONE-WHEN'
        root.done = hval.empty? ? nil : hval
      elsif key == 'SOURCE'
        root.source = hval.empty? ? nil : hval
      elsif key == 'DEADLINE'
        m = DEADLINE_RE.match(hval.strip)
        raise CardError, 'malformed' if m.nil?

        digits = (m[1] || '').sub(/\A0+/, '')
        digits = '0' if digits.empty?
        raise CardError, 'malformed' if digits.length > 6 || (digits.length == 6 && digits > '100000')

        root.deadline_minutes = Integer(digits, 10)
      elsif !RESERVED[key]
        check_text!(key, 32)
        kv = Cards::KeyValue.new
        kv.key = key
        kv.value = hval
        root.extra << kv
      end
      body_at = line_end
    end
    rem = text[body_at..] || ''
    root.body = rem.empty? ? nil : rem
    Record.new(save_card(root), root, Cards::LoadReport.new)
  end

  def self.render(record)
    root = load(record.wire).root
    tier = Cards::Tier::NAMES[root.tier]
    raise CardError, 'malformed' if tier.nil? || root.tier.zero?

    out = [+"RESULT: #{root.id} sha=#{root.sha} tier: #{tier}\n"]
    [%w[REPO repo], %w[BASE _base], %w[KIND kind]].each do |key, fname|
      val = root.public_send(fname.to_sym)
      out << "#{key}: #{val}\n" unless val.empty?
    end
    out << "PRIORITY: #{Cards::Priority::NAMES[root.priority]}\n" unless root.priority.zero?
    [%w[PATHS paths], %w[NEW _new], %w[SHARED shared], %w[DEPENDS-ON needs]].each do |key, fname|
      vals = root.public_send(fname.to_sym)
      out << "#{key}: #{vals.map(&:s).join(', ')}\n" unless vals.empty?
    end
    out << "TEST: #{root.test}\n" unless root.test.empty?
    done = blob_text(root.done)
    out << "DONE-WHEN: #{done}\n" unless done.nil?
    out << "DEADLINE: finish within #{root.deadline_minutes} minutes.\n" unless root.deadline_minutes.zero?
    source = blob_text(root.source)
    out << "SOURCE: #{source}\n" unless source.nil?
    root.extra.each do |item|
      out << "#{item.key}: #{blob_text(item.value) || ''}\n"
    end
    body = blob_text(root.body)
    out << body unless body.nil?
    out.join
  end

  def self.frame_bytes(str)
    raw = str.b
    [raw.bytesize].pack('Q<') + raw
  end

  def self.task_hash(record)
    raise CardError, 'unknown-present' if record.report.unknown.positive? || record.report.retain_lost.positive?

    root = record.root
    data = +frame_bytes(blob_text(root.body) || '')
    %w[paths _new shared].each do |fname|
      items = root.public_send(fname.to_sym)
      data << [items.length].pack('Q<')
      items.each { |item| data << frame_bytes(item.s) }
    end
    data << frame_bytes(root.test)
    data << frame_bytes(blob_text(root.done) || '')
    Digest::SHA256.hexdigest(data)
  end

  def self.edit(record, name, value)
    raise CardError, 'unknown-present' if record.report.unknown.positive? || record.report.retain_lost.positive?

    root = load(record.wire).root
    case name
    when 'paths', 'needs'
      values = if value.is_a?(String)
                 value.empty? ? [] : value.split(',', -1)
               elsif value.is_a?(Array)
                 value
               else
                 raise CardError, 'malformed'
               end
      raise CardError, "list-bound #{name} 512" if values.length > MAX_LIST_ENTRIES

      bound = name == 'paths' ? 192 : 64
      cls = name == 'paths' ? Cards::PathText : Cards::CardID
      values.each { |part| check_text!(part, bound) }
      root.public_send(:"#{name}=", values.map { |part| build_row(cls, part) })
    when 'tier', 'priority', 'column'
      raise CardError, 'malformed' unless value.is_a?(String)

      variants = { 'tier' => TIERS, 'priority' => PRIORITIES, 'column' => COLUMNS }[name]
      raise CardError, 'malformed' unless variants.key?(value)

      root.public_send(:"#{name}=", variants[value])
    when 'holder'
      raise CardError, 'malformed' unless value.is_a?(String)

      check_text!(value, 32)
      root.holder = value
    else
      raise CardError, 'malformed'
    end
    wire = save_card(root)
    record.root = root
    record.wire = wire
    nil
  end

  def self.set_paths(record, values)
    edit(record, 'paths', values)
  end

  def self.set_needs(record, values)
    edit(record, 'needs', values)
  end

  def self.set_tier(record, value)
    edit(record, 'tier', value)
  end

  def self.set_priority(record, value)
    edit(record, 'priority', value)
  end

  def self.set_column(record, value)
    edit(record, 'column', value)
  end

  def self.set_holder(record, value)
    edit(record, 'holder', value)
  end

  def self.upgrade(wire)
    retain = Cards::TableRetain.new("\x00".b * MAX_RETAIN_BYTES, "\x00".b * (MAX_RETAIN_IDS * 24))
    report = Cards::LoadReport.new
    raw = wire.dup.force_encoding(Encoding::BINARY)
    root = Cards::Card.load_retain(raw, retain, report)
    check_card!(root)
    raise CardError, 'retain-lost' if report.retain_lost.positive?

    output = root.save_retain(retain, report)
    raise CardError, 'retain-lost' if report.retain_lost.positive?

    [output, report]
  rescue Cards::Refused, Cards::InvalidValue
    raise CardError, 'malformed'
  end
end
