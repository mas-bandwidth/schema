# frozen_string_literal: true

# The cross-language corpus instrument (STANDARD.md, "The conformance corpus").
#
# Enumerates conformance/ at run time and runs every record as STANDARD.md,
# "What a runner checks", requires:
#
# * an accepted vector decodes the stated value (a bit pattern for the float
#   family) and consumes exactly `consumed` bits;
# * `writer canonical` re-writes the decoded values to the vector's bytes;
# * `writer_input bits = 0x...` hands the compressed_float writer that float32
#   and requires the vector's bytes;
# * a refused vector refuses with its stated reason (the stream's first
#   refusal), leaves the failing read's destination unwritten, and leaves the
#   stream terminal: every later step refuses too, consuming no bits and
#   writing nothing, and so does one more read of every step after the
#   record, and an end;
# * `measure_at_least` is a floor for MeasureStream.
#
# Vector names are unique across the corpus; a malformed record (an unknown
# key, a refusal with no reason or an unknown one) is red, and an operation
# with no runner is red, never skipped.
#
# The buffer contract: a ReadStream reads its bytes and never looks past them,
# so the stream is presented with no slack.
#
# THE CONTROLS (STANDARD.md, "A gate nobody has seen go red"): the vendored
# conformance-controls/unknown-operation.txt must be rejected; and for each
# refusal rule, a reader with that one rule deleted (a copy of
# lib/serialize/core.rb with one needle replaced, evaluated inside a fresh
# module in place of the real library) runs the real corpus and must fail
# exactly the vectors conformance-controls/<rule>.expected names. A needle
# that does not match exactly once is a failure of the control.
#
# THE CORPUS ROOT: the directory holding conformance/ and
# conformance-controls/. It is this repository's root unless
# SERIALIZE_CORPUS_DIR names another. A root that holds no vectors is red, by
# name.

require 'serialize'

module Conformance
  ROOT = File.dirname(__dir__)
  CORE_SOURCE = File.join(ROOT, 'lib', 'serialize', 'core.rb')

  # the corpus root for a SERIALIZE_CORPUS_DIR value; unset or empty is ROOT
  def self.corpus_root_of(value)
    value.nil? || value.empty? ? ROOT : value
  end

  CORPUS_ROOT = corpus_root_of(ENV.fetch('SERIALIZE_CORPUS_DIR', nil))

  # a vector the runner refuses: a disagreement, or a malformed record
  class RunnerError < StandardError; end

  def self.check(condition, message)
    raise RunnerError, message unless condition
  end

  # the library under test; a control swaps in a sabotaged copy
  @lib = Serialize
  class << self
    attr_accessor :lib
  end

  # ---------------------------------------------------------------------------
  # numbers and bytes (STANDARD.md, "The vector format")
  # ---------------------------------------------------------------------------

  HEX = /\A[0-9a-fA-F]+\z/

  def self.parse_int(token)
    t = token.strip
    check(t != '', 'malformed number: empty')
    neg = t.start_with?('-')
    body = neg ? t[1..] : t
    if body.start_with?('0x', '0X')
      digits = body[2..]
      check(digits.size <= 32 && HEX.match?(digits), "malformed hexadecimal number: #{token}")
      v = digits.to_i(16)
    else
      check(/\A[0-9]+\z/.match?(body), "malformed decimal number: #{token}")
      v = body.to_i
    end
    v = -v if neg
    check(v > -(1 << 128) && v < (1 << 128), "number wider than 128 bits: #{token}")
    v
  end

  def self.parse_float_param(token)
    Float(token)
  rescue ArgumentError
    raise RunnerError, "malformed float parameter: #{token}"
  end

  def self.unhex(text)
    compact = text.split.join
    check(compact.size.even? && (compact.empty? || HEX.match?(compact)), "malformed hex bytes: #{text}")
    [compact].pack('H*')
  end

  def self.hex_of(data)
    data.unpack('C*').map { |b| format('%02X', b) }.join(' ')
  end

  # ---------------------------------------------------------------------------
  # records
  # ---------------------------------------------------------------------------

  KNOWN_KEYS = %w[operation name param bytes expect consumed writer writer_input measure_at_least].freeze

  Record = Struct.new(:operation, :file, :line, :name, :params, :data, :expect, :reason, :expect_text,
                      :consumed, :writer, :writer_input, :measure) do
    def param(wanted)
      pair = params.find { |n, _| n == wanted }
      pair && pair[1]
    end
  end

  def self.new_record(operation, file, line)
    Record.new(operation, file, line, nil, [], nil, nil, nil, '', nil, false, nil, nil)
  end

  def self.parse_vectors_text(content, path)
    records = []
    cur = nil
    content.split("\n", -1).each_with_index do |line, i|
      where = "#{path}:#{i + 1}"
      if line.strip.empty?
        cur = nil
        next
      end
      next if line.start_with?('#')

      key, _, rest = line.strip.partition(' ')
      rest = rest.strip
      check(KNOWN_KEYS.include?(key), "#{where}: unknown key #{key}")
      if key == 'operation'
        check(cur.nil?, "#{where}: a second operation in one record")
        cur = new_record(rest, path, i + 1)
        records << cur
        next
      end
      check(!cur.nil?, "#{where}: #{key} outside a record")
      parse_line(cur, key, rest, where)
    end
    records.each do |rec|
      where = "#{path}:#{rec.line}"
      check(!rec.name.nil?, "#{where}: a record has no name")
      check(!rec.expect.nil?, "#{where}: #{rec.name} has no expect")
      check(!rec.data.nil?, "#{where}: #{rec.name} has no bytes")
      check(!rec.consumed.nil?, "#{where}: #{rec.name} has no consumed") unless rec.expect == 'refused'
    end
    records
  end

  def self.parse_line(cur, key, rest, where)
    case key
    when 'name'
      check(cur.name.nil? && rest != '', "#{where}: name")
      cur.name = rest
    when 'param'
      pname, eq, pval = rest.partition('=')
      check(eq == '=' && pname.strip != '', "#{where}: malformed param")
      cur.params << [pname.strip, pval.strip]
    when 'bytes'
      cur.data = unhex(rest)
    when 'expect'
      parse_expect(cur, rest, where)
    when 'consumed'
      check(/\A[0-9]+\z/.match?(rest), "#{where}: malformed consumed")
      cur.consumed = rest.to_i
    when 'writer'
      check(rest == 'canonical', "#{where}: writer #{rest}")
      cur.writer = true
    when 'writer_input'
      k, eq, v = rest.partition('=')
      v = v.strip
      check(k.strip == 'bits' && eq == '=' && v.start_with?('0x') && v.size == 10 && HEX.match?(v[2..]),
            "#{where}: malformed writer_input")
      cur.writer_input = v[2..].to_i(16)
    when 'measure_at_least'
      check(/\A[0-9]+\z/.match?(rest), "#{where}: malformed measure_at_least")
      cur.measure = rest.to_i
    end
  end

  def self.parse_expect(cur, rest, where)
    check(cur.expect.nil?, "#{where}: a second expect")
    if rest == 'refused' || rest.start_with?('refused ')
      reason = rest.delete_prefix('refused').strip
      check(reason != '', "#{where}: a refusal states no reason")
      check(lib::REASONS.map(&:to_s).include?(reason), "#{where}: unknown refusal reason #{reason}")
      cur.expect = 'refused'
      cur.reason = reason.to_sym
    else
      kind, eq, value = rest.partition('=')
      kind = kind.strip
      check(eq == '=' && %w[value bits].include?(kind), "#{where}: malformed expect #{rest}")
      cur.expect = kind
      cur.expect_text = value.strip
    end
  end

  def self.parse_vectors(path)
    parse_vectors_text(File.read(path, encoding: 'UTF-8'), path)
  end

  # ---------------------------------------------------------------------------
  # steps: one spelling per operation (conformance/sequence.txt)
  # ---------------------------------------------------------------------------

  Step = Struct.new(:op, :args, :unstated, :index, :children)

  def self.new_step(op, args, unstated: false)
    Step.new(op, args, unstated, 0, [])
  end

  ARITY = {
    'bits' => 1, 'bool' => 0, 'align' => 0, 'float' => 0, 'double' => 0, 'uint128' => 0, 'end' => 0,
    'int' => 2, 'int64' => 2, 'int128' => 2, 'fixed' => 4, 'int_relative' => 1, 'compressed_float' => 3,
    'bytes' => 1, 'string' => 1, 'object' => 1, 'reject' => 0
  }.freeze

  def self.parse_step(op, words)
    raise RunnerError, "no runner for operation #{op}" unless ARITY.key?(op)

    check(words.size == ARITY[op], "step #{op} takes #{ARITY[op]} parameters, got #{words.size}")
    return new_step(op, words.map { |w| parse_float_param(w) }) if op == 'compressed_float'

    new_step(op, words.map { |w| parse_int(w) })
  end

  # the wire operations a single-operation record may name; reject and object
  # are steps only
  KNOWN_OPS = (ARITY.keys - %w[object reject]).freeze

  SINGLE_PARAMS = {
    'bits' => %w[bits],
    'int' => %w[min max],
    'int64' => %w[min max],
    'int128' => %w[min max],
    'fixed' => %w[integer_bits fraction_bits min max],
    'int_relative' => %w[previous],
    'compressed_float' => %w[min max res],
    'bytes' => %w[count],
    'string' => %w[buffer_size]
  }.freeze

  # the flat step list, and the index of the step a single record's value is for
  def self.build_steps(rec)
    name = rec.name || '?'
    if rec.operation == 'sequence'
      steps = rec.params.map do |pname, pval|
        check(pname == 'step', "#{name}: a sequence takes only step params, got #{pname}")
        words = pval.split
        check(!words.empty?, "#{name}: an empty step")
        parse_step(words[0], words[1..])
      end
      check(!steps.empty?, "#{name}: a sequence with no steps")
      return [steps, nil]
    end
    op = rec.operation
    raise RunnerError, "no runner for operation #{op}" unless KNOWN_OPS.include?(op)

    values = SINGLE_PARAMS.fetch(op, []).map do |pname|
      v = rec.param(pname)
      check(!v.nil?, "#{name}: param #{pname} missing")
      v
    end
    used = values.size
    rec.params.each { |pair| check(pair[0] != 'step', "#{name}: a step param outside a sequence") }
    steps = []
    pb = rec.param('preceding_bits')
    unless pb.nil?
      used += 1
      steps << new_step('bits', [parse_int(pb)], unstated: true)
    end
    check(used == rec.params.size, "#{name}: unexpected params for #{op}")
    steps << parse_step(op, values)
    [steps, steps.size - 1]
  end

  # `object <n>` wraps the next n steps
  def self.tree_of(steps)
    pos = 0
    take = lambda do |limit|
      nodes = []
      while pos < steps.size && (limit.nil? || nodes.size < limit)
        st = steps[pos]
        st.index = pos
        pos += 1
        if st.op == 'object'
          st.children = take.call(st.args[0])
          check(st.children.size == st.args[0], "object #{st.args[0]} wraps fewer steps")
        end
        nodes << st
      end
      nodes
    end
    take.call(nil)
  end

  # ---------------------------------------------------------------------------
  # running steps
  # ---------------------------------------------------------------------------

  SENTINEL = Object.new
  def SENTINEL.inspect = '<unwritten>'

  def self.call(s, st, holder)
    a = st.args
    case st.op
    when 'bits'
      if a[0].zero?
        # zero bits read and write nothing on a healthy stream; a failed one
        # answers false (terminal)
        if s.reading?
          return false unless s.refusal.nil?

          holder.value = 0
        end
        return true
      end
      s.serialize_bits(holder, a[0]) == true
    when 'bool' then s.serialize_bool(holder) == true
    when 'uint128' then s.serialize_uint128(holder) == true
    when 'int' then s.serialize_int(holder, a[0], a[1]) == true
    when 'int64' then s.serialize_int64(holder, a[0], a[1]) == true
    when 'int128' then s.serialize_int128(holder, a[0], a[1]) == true
    when 'fixed' then s.serialize_fixed(holder, a[0], a[1], a[2], a[3]) == true
    when 'int_relative' then s.serialize_int_relative(a[0], holder) == true
    when 'float' then s.serialize_float(holder) == true
    when 'double' then s.serialize_double(holder) == true
    when 'compressed_float' then s.serialize_compressed_float(holder, a[0], a[1], a[2]) == true
    when 'bytes' then s.serialize_bytes(holder, a[0]) == true
    when 'string' then s.serialize_string(holder, a[0]) == true
    when 'align' then s.serialize_align == true
    when 'end' then s.serialize_end == true
    else raise RunnerError, "no runner for operation #{st.op}"
    end
  end

  Result = Struct.new(:step, :ok, :consumed, :holder)

  # reads every node, never stopping at a wire refusal, recording per step
  # whether it passed, the bits it consumed, and its destination
  def self.read_tree(s, nodes, out, in_object: false)
    all_ok = true
    nodes.each do |st|
      before = s.bits_processed
      r = Result.new(st, false, 0, lib::Ref.new(SENTINEL))
      out[st.index] = r
      case st.op
      when 'object'
        r.ok = s.serialize_object { |stream| read_tree(stream, st.children, out, in_object: true) } == true
      when 'reject'
        if in_object
          r.ok = false
          return false
        end
        r.ok = s.reject == true
      else
        r.ok = call(s, st, r.holder)
      end
      r.consumed = s.bits_processed - before
      all_ok &&= r.ok
    end
    all_ok
  end

  def self.write_tree(s, nodes, values)
    nodes.each do |st|
      check(st.op != 'reject', 'a reject step appears only in refused vectors')
      if st.op == 'object'
        return false unless s.serialize_object { |stream| write_tree(stream, st.children, values) }
      elsif !call(s, st, lib::Ref.new(values[st.index]))
        return false
      end
    end
    true
  end

  def self.capacity_for(count)
    [8, (count + 7) / 8 * 8].max
  end

  FLOAT_OPS = %w[float double compressed_float].freeze

  def self.matches(st, actual, token, kind)
    op = st.op
    return %w[- 0].include?(token) if %w[align end object reject].include?(op)
    return false if token == '-'

    if FLOAT_OPS.include?(op)
      return false unless token.start_with?('0x') && actual.is_a?(Float)

      got = op == 'double' ? lib.float64_bits(actual) : lib.float32_bits(actual)
      return got == token[2..].to_i(16)
    end
    return false if kind == 'bits'

    case op
    when 'bool' then (token == 'true' && actual == true) || (token == 'false' && actual == false)
    when 'bytes', 'string' then actual.is_a?(String) && actual.b == unhex(token)
    else actual.is_a?(Integer) && actual == parse_int(token)
    end
  end

  def self.render(st, value)
    return '<unwritten>' if value.equal?(SENTINEL)
    return "[#{hex_of(value)}]" if value.is_a?(String)

    if value.is_a?(Float)
      return format('0x%016X', lib.float64_bits(value)) if st.op == 'double'

      return format('0x%08X', lib.float32_bits(value))
    end
    value.inspect
  end

  def self.split_expect(text, count)
    count == 1 ? [text] : text.split('|').map(&:strip)
  end

  def self.run_writer_input(rec, steps)
    check(steps.size == 1 && steps[0].op == 'compressed_float',
          "#{rec.name}: writer_input is for a single compressed_float")
    a = steps[0].args
    ws = lib::WriteStream.new(capacity_for(rec.data.bytesize))
    value = lib.float32_from_bits(rec.writer_input)
    check(ws.serialize_compressed_float(lib::Ref.new(value), a[0], a[1], a[2]) == true,
          "#{rec.name}: writer_input write failed")
    ws.flush
    got = ws.data
    check(got == rec.data, "#{rec.name}: writer_input wrote [#{hex_of(got)}], want [#{hex_of(rec.data)}]")
  end

  # Over each refused object.txt vector's bytes: an object whose callback
  # returns false with nothing refused inside latches rejected, and the stream
  # is terminal after it.
  def self.check_callback_refuses(rec)
    rs = lib::ReadStream.new(rec.data)
    check(rs.serialize_object { |_| false } == false,
          "#{rec.name}: an object whose callback returns false was accepted")
    check(rs.refusal == :rejected,
          "#{rec.name}: an object callback's own refusal latched #{rs.refusal.inspect}, not rejected")
    probe = lib::Ref.new(SENTINEL)
    check(rs.serialize_bits(probe, 8) == false && probe.value.equal?(SENTINEL) && rs.bits_processed.zero?,
          "#{rec.name}: a read after the callback's refusal was not refused cleanly")
    called = false
    accepted = rs.serialize_object do |_|
      called = true
      true
    end
    check(accepted == false && !called,
          "#{rec.name}: an object after the callback's refusal was accepted or ran its callback")
    check(rs.refusal == :rejected, "#{rec.name}: the echoes changed rejected to #{rs.refusal.inspect}")
  end

  def self.run_record(rec)
    steps, single = build_steps(rec)
    tree = tree_of(steps)
    run_writer_input(rec, steps) unless rec.writer_input.nil?

    rs = lib::ReadStream.new(rec.data)
    out = {}
    ok = read_tree(rs, tree, out)
    return check_refused(rec, steps, tree, rs, out, ok) if rec.expect == 'refused'

    check_accepted(rec, steps, single, tree, rs, out, ok)
  end

  def self.check_refused(rec, steps, tree, rs, out, ok)
    name = rec.name
    check(!ok, "#{name}: a refused vector was accepted")
    check(rs.refusal == rec.reason, "#{name}: refused as #{rs.refusal.inspect}, the vector states #{rec.reason}")
    first = nil
    steps.each_index do |i|
      r = out[i]
      next if r.nil?

      if first.nil?
        next if r.ok || steps[i].op == 'object'

        first = i
        check(r.holder.value.equal?(SENTINEL),
              "#{name}: step #{i} refused but wrote its destination: #{render(steps[i], r.holder.value)}")
      else
        check(!r.ok, "#{name}: step #{i} after the refusal was accepted")
        check(r.consumed.zero?, "#{name}: step #{i} after the refusal consumed bits")
        check(r.holder.value.equal?(SENTINEL), "#{name}: step #{i} after the refusal wrote its destination")
      end
    end
    check(!first.nil?, "#{name}: no primitive step refused")
    before = rs.bits_processed
    again = {}
    check(!read_tree(rs, tree, again), "#{name}: the failed stream accepted a further read")
    steps.each_index do |i|
      r = again[i]
      next if r.nil?

      check(!r.ok && r.consumed.zero? && r.holder.value.equal?(SENTINEL),
            "#{name}: a further read of step #{i} on the failed stream was not refused cleanly")
    end
    check(rs.serialize_end == false, "#{name}: an end on the failed stream was accepted")
    check(rs.bits_processed == before, "#{name}: the failed stream consumed bits")
    check(rs.refusal == rec.reason, "#{name}: the echoes changed the first reason to #{rs.refusal.inspect}")
    check_callback_refuses(rec) if rec.file.end_with?('object.txt')
  end

  def self.check_accepted(rec, steps, single, tree, rs, out, ok)
    name = rec.name
    check(ok, "#{name}: an accepted vector was refused as #{rs.refusal.inspect}")
    check(rs.bits_processed == rec.consumed,
          "#{name}: consumed #{rs.bits_processed}, the vector states #{rec.consumed}")
    values = steps.each_index.to_h { |i| [i, out[i].holder.value] }
    kind = rec.expect
    if single.nil?
      parts = split_expect(rec.expect_text, steps.size)
      check(parts.size == steps.size, "#{name}: #{parts.size} expected values for #{steps.size} steps")
      steps.each_with_index do |st, i|
        check(matches(st, values[i], parts[i], kind),
              "#{name}: step #{i} (#{st.op}) decoded #{render(st, values[i])}, the vector states #{parts[i]}")
      end
    else
      steps.each_index { |i| check(steps[i].unstated, "#{name}: a value with no expectation") if i != single }
      check(matches(steps[single], values[single], rec.expect_text, kind),
            "#{name}: decoded #{render(steps[single], values[single])}, the vector states #{rec.expect_text}")
    end
    check_writer(rec, tree, values) if rec.writer
    check_measure(rec, tree, values) unless rec.measure.nil?
  end

  def self.check_writer(rec, tree, values)
    ws = lib::WriteStream.new(capacity_for(rec.data.bytesize))
    check(write_tree(ws, tree, values), "#{rec.name}: the canonical write failed")
    check(ws.serialize_end == true, "#{rec.name}: the writer's end failed")
    ws.flush
    got = ws.data
    check(got == rec.data, "#{rec.name}: the writer emitted [#{hex_of(got)}], the vector states [#{hex_of(rec.data)}]")
  end

  def self.check_measure(rec, tree, values)
    ms = lib::MeasureStream.new
    check(write_tree(ms, tree, values), "#{rec.name}: the measure failed")
    check(ms.bits_processed >= rec.measure,
          "#{rec.name}: measured #{ms.bits_processed}, the vector requires at least #{rec.measure}")
  end

  # ---------------------------------------------------------------------------
  # the corpus
  # ---------------------------------------------------------------------------

  CorpusRun = Struct.new(:counts, :total, :failed) # counts: [file, records, accepted, refused]

  def self.run_corpus(root = nil)
    directory = File.join(root || CORPUS_ROOT, 'conformance')
    files = File.directory?(directory) ? Dir.children(directory).select { |f| f.end_with?('.txt') }.sort : []
    check(!files.empty?, "no conformance vectors found: #{directory}/ is missing or empty")
    names = {}
    run = CorpusRun.new([], 0, [])
    files.each do |file|
      path = File.join(directory, file)
      records = parse_vectors(path)
      declared = File.foreach(path).count { |line| line.start_with?('operation ') }
      check(!records.empty? && records.size == declared,
            "#{file}: parsed #{records.size} records, the file declares #{declared}")
      accepted = 0
      records.each do |rec|
        check(!names.key?(rec.name), "#{file}: vector name #{rec.name} is not unique")
        names[rec.name] = file
        begin
          run_record(rec)
        rescue StandardError => e # a sabotaged library may raise anything
          run.failed << [rec.name, "#{e.class}: #{e.message}"]
        end
        accepted += 1 unless rec.expect == 'refused'
        run.total += 1
      end
      run.counts << [file, records.size, accepted, records.size - accepted]
    end
    run
  end

  # ---------------------------------------------------------------------------
  # the sabotage controls
  # ---------------------------------------------------------------------------

  Patch = Struct.new(:anchors, :needle, :replacement)
  Control = Struct.new(:name, :rule, :patches)

  READ = 'class ReadStream < Stream'

  def self.patch(anchors, needle, replacement)
    Patch.new(anchors, needle, replacement)
  end

  CONTROLS = [
    Control.new(
      'align-padding', 'readers must verify that the alignment padding bits are zero',
      [patch([READ, 'def check_align'], 'if pad != 0 && take(pad) != 0', 'if pad != 0 && take(pad) != 0 && false')]
    ),
    Control.new(
      'ranged-reconstruction', 'a ranged int decodes to its offset plus min',
      [
        patch([READ, 'def read_int32('], 'min + offset', 'offset'),
        patch([READ, 'def serialize_int_relative('], 'current = previous + lo + offset', 'current = previous + offset')
      ]
    ),
    Control.new(
      'ranged-range', 'a ranged int whose offset is past the span is refused',
      [
        patch([READ, 'def read_int32('], 'if offset > span', 'if offset > span && false'),
        patch([READ, 'def serialize_int_relative('], 'if offset > hi - lo', 'if offset > hi - lo && false')
      ]
    ),
    Control.new(
      'fixed-degenerate-terminal', 'every read consults the failure state, zero bit reads included',
      [patch([READ, 'def serialize_fixed('], "return false if @err\n", '')]
    ),
    Control.new(
      'float-finite', 'a float read refuses NaN and the infinities',
      [patch([READ, 'def serialize_float('], 'if bits.allbits?(0x7F800000)', 'if bits.allbits?(0x7F800000) && false')]
    ),
    Control.new(
      'double-finite', 'a double read refuses NaN and the infinities',
      [patch([READ, 'def serialize_double('], 'if bits.allbits?(0x7FF0000000000000)',
             'if bits.allbits?(0x7FF0000000000000) && false')]
    ),
    Control.new(
      'compressed-float-finite', 'a compressed float decode that is not finite is refused',
      [patch([READ, 'def read_compressed('], 'unless decoded.finite?', 'unless decoded.finite? || true')]
    ),
    Control.new(
      'end-trailing-data', 'an end refuses a whole byte after the object',
      [patch([READ, 'def serialize_end'], 'if remaining >= 8', 'if remaining >= 8 && false')]
    ),
    Control.new(
      'end-padding', 'an end refuses non-zero padding in the final byte',
      [patch([READ, 'def serialize_end'], 'if remaining > 0 && take(remaining) != 0',
             'if remaining > 0 && take(remaining) != 0 && false')]
    ),
    Control.new(
      'int-relative-canonical', 'the absolute tier refuses a difference a bounded tier carries',
      [patch([READ, 'def serialize_int_relative('], 'if v - previous <= LAST_BOUNDED_MAX',
             'if v - previous <= LAST_BOUNDED_MAX && false')]
    ),
    Control.new(
      'string-embedded-nul', 'a string read refuses a zero byte among its payload',
      [patch([READ, 'def serialize_string('], 'if payload.include?("\0")', 'if payload.include?("\0") && false')]
    ),
    Control.new(
      'string-utf8', 'a string read refuses a payload that is not well formed UTF-8',
      [patch([READ, 'def serialize_string('], 'unless text.valid_encoding?', 'if false')]
    ),
    Control.new(
      # a reader that takes an encoded surrogate code point as well-formed,
      # as a decoder of CESU-8 or WTF-8 would, and is strict otherwise
      'string-utf8-surrogate', 'a string read refuses an encoded surrogate code point',
      [patch([READ, 'def serialize_string('], 'unless text.valid_encoding?',
             "unless text.valid_encoding? || text.b.gsub(/\\xED[\\xA0-\\xBF][\\x80-\\xBF]/n, 'a')" \
             '.force_encoding(Encoding::UTF_8).valid_encoding?')]
    ),
    Control.new(
      # a reader whose ranged 64-bit integer passes through a double, as a
      # port backed by a double number type would
      'int64-through-double', 'a 64 bit value is carried exactly, never through a double',
      [patch([READ, 'def serialize_int64('], 'ref.value = min + offset',
             'ref.value = (min + offset).to_f < 9223372036854775807.0 ? (min + offset).to_f.to_i : INT64_MAX')]
    ),
    Control.new(
      # serialize_object consults the latch on entry and on exit, and either
      # alone refuses an empty object on a failed stream, so the control
      # deletes both
      'object-terminal', 'a nested object on a failed stream refuses before its callback runs',
      [patch([READ, 'def serialize_object'],
             "return false if @err\n      return true if yield(self) == true && @err.nil?",
             'return true if yield(self) == true')]
    ),
    Control.new(
      'object-latch-rejected', 'a nested object that refuses on its own latches rejected',
      [patch([READ, 'def serialize_object'], 'refuse(REJECTED)', 'false')]
    ),
    Control.new(
      'reject-reason', 'the reject entry point refuses as rejected',
      [patch([READ, "def reject\n"], 'refuse(REJECTED)', 'refuse(VALUE_OUT_OF_RANGE)')]
    ),
    Control.new(
      'wrong-reason', 'a refusal names the rule that refused it',
      [patch([READ, 'def serialize_string('], 'return refuse(EMBEDDED_NUL) if payload.include?("\0")',
             'return refuse(INVALID_UTF8) if payload.include?("\0")')]
    )
  ].freeze

  def self.sabotaged_source(control, source)
    control.patches.each do |p|
      at = 0
      p.anchors.each do |anchor|
        found = source.index(anchor, at)
        check(!found.nil?, "control #{control.name}: the anchor #{anchor.inspect} no longer matches")
        at = found + anchor.size
      end
      # the needle's region ends at the next definition at the anchor's level
      stop = source.index("\n    def ", at) || source.size
      first = source.index(p.needle, at)
      check(!first.nil? && first + p.needle.size <= stop,
            "control #{control.name}: the needle no longer matches #{p.needle.inspect}")
      again = source.index(p.needle, first + 1)
      check(again.nil? || again + p.needle.size > stop,
            "control #{control.name}: the needle matches more than once #{p.needle.inspect}")
      source = source[0...first] + p.replacement + source[(first + p.needle.size)..]
    end
    source
  end

  # the library with the control's rule deleted, evaluated inside a fresh
  # module so the real Serialize is untouched
  def self.load_sabotaged(control)
    source = sabotaged_source(control, File.read(CORE_SOURCE, encoding: 'UTF-8'))
    holder = Module.new
    holder.module_eval(source, "#{CORE_SOURCE} (sabotaged: #{control.name})", 1)
    holder.const_get(:Serialize, false)
  end

  def self.with_library(mod)
    saved = lib
    self.lib = mod
    yield
  ensure
    self.lib = saved
  end

  def self.read_expected(name)
    path = File.join(CORPUS_ROOT, 'conformance-controls', "#{name}.expected")
    File.readlines(path, chomp: true, encoding: 'UTF-8').map(&:strip).reject { |l| l.empty? || l.start_with?('#') }
  end

  # the problems with one control: empty when exactly its list failed
  def self.run_control(control)
    expected = read_expected(control.name)
    check(!expected.empty?, "control #{control.name}: the expected list names no vector")
    run = with_library(load_sabotaged(control)) { run_corpus }
    failed = run.failed.to_h
    problems = expected.reject { |n| failed.key?(n) }.map { |n| "#{n} should have failed and passed" }
    problems + failed.keys.sort.reject { |n| expected.include?(n) }.map do |n|
      "#{n} failed and is not in the list: #{failed[n]}"
    end
  end

  # ---------------------------------------------------------------------------
  # the runner's own reversed witnesses: each is a real vector with one thing
  # wrong, which the runner must fail, beside its twin with that thing right
  # ---------------------------------------------------------------------------

  WITNESSES = [
    [
      "operation uint128\nname w\nbytes FF FF FF FF FF FF FF FF 00 00 00 00 00 00 00 00\n" \
      "expect value = 18446744073709551615\nconsumed 128\nwriter canonical\n",
      "operation uint128\nname w\nbytes FF FF FF FF FF FF FF FF 00 00 00 00 00 00 00 00\n" \
      "expect value = -1\nconsumed 128\n",
      'a 128-bit value compared on both halves'
    ],
    [
      "operation bits\nname w\nparam bits = 64\nbytes FF FF FF FF FF FF FF FF\n" \
      "expect value = 18446744073709551615\nconsumed 64\n",
      "operation bits\nname w\nparam bits = 64\nbytes FF FF FF FF FF FF FF FF\n" \
      "expect value = 18446744073709551614\nconsumed 64\n",
      'a 64-bit value compared exactly'
    ],
    [
      "operation int64\nname w\nparam min = -9223372036854775808\nparam max = 9223372036854775807\n" \
      "bytes FF FF FF FF FF FF FF FF\nexpect value = 9223372036854775807\nconsumed 64\n",
      "operation int64\nname w\nparam min = -9223372036854775808\nparam max = 9223372036854775807\n" \
      "bytes FF FF FF FF FF FF FF FF\nexpect value = 9223372036854775807\nconsumed 63\n",
      'the bits consumed'
    ],
    [
      "operation sequence\nname w\nparam step = bits 3\nbytes 05\nexpect value = 5\nconsumed 3\nwriter canonical\n",
      "operation sequence\nname w\nparam step = bits 3\nbytes 0D\nexpect value = 5\nconsumed 3\nwriter canonical\n",
      "the canonical writer's bytes"
    ],
    [
      "operation string\nname w\nparam buffer_size = 4\nbytes 01 00\nexpect refused embedded_nul\n",
      "operation string\nname w\nparam buffer_size = 4\nbytes 01 00\nexpect refused invalid_utf8\n",
      'the stated refusal reason'
    ],
    [
      "operation sequence\nname w\nparam step = bits 8\nparam step = align\nparam step = bits 8\nbytes 01 02\n" \
      "expect value = 1 | - | 2\nconsumed 16\nmeasure_at_least 23\n",
      "operation sequence\nname w\nparam step = bits 8\nparam step = align\nparam step = bits 8\nbytes 01 02\n" \
      "expect value = 1 | - | 2\nconsumed 16\nmeasure_at_least 24\n",
      'the measure floor'
    ],
    [
      "operation compressed_float\nname w\nparam min = 0\nparam max = 10\nparam res = 0.01\n" \
      "writer_input bits = 0x3CCCCCCD\nbytes 03 00\nexpect bits = 0x3CF5C290\nconsumed 10\n",
      "operation compressed_float\nname w\nparam min = 0\nparam max = 10\nparam res = 0.01\n" \
      "writer_input bits = 0x3CA3D70A\nbytes 03 00\nexpect bits = 0x3CF5C290\nconsumed 10\n",
      'the writer_input bytes'
    ],
    [
      "operation sequence\nname w\nparam step = bits 3\nparam step = end\nbytes 05\nexpect value = 5 | -\nconsumed 8\n",
      "operation sequence\nname w\nparam step = bits 3\nparam step = end\nbytes 05\nexpect value = 6 | -\nconsumed 8\n",
      "a sequence's values"
    ]
  ].freeze
end

class ConformanceTest < Harness::TestCase
  C = Conformance

  def test_corpus
    run = C.run_corpus
    lines = ["conformance: corpus root #{C::CORPUS_ROOT}"]
    run.counts.each do |file, n, accepted, refused|
      lines << "  #{file}: #{n} records, #{accepted} accepted, #{refused} refused"
    end
    run.failed.each { |name, err| lines << "  FAIL #{name}: #{err}" }
    lines << "conformance: #{run.total - run.failed.size} of #{run.total} vectors passed in #{run.counts.size} files"
    puts lines.join("\n")
    assert_operator(run.total, :>, 0)
    assert_equal([], run.failed, "#{run.failed.size} of #{run.total} vectors failed")
  end

  def test_control_unknown_operation
    recs = C.parse_vectors(File.join(C::CORPUS_ROOT, 'conformance-controls', 'unknown-operation.txt'))
    assert_equal(1, recs.size)
    assert_raises(C::RunnerError, /no runner for operation no_such_operation/) { C.run_record(recs[0]) }
  end

  def test_controls
    names = C::CONTROLS.map(&:name)
    listed = Dir.children(File.join(C::CORPUS_ROOT, 'conformance-controls'))
                .select { |f| f.end_with?('.expected') }.map { |f| f.delete_suffix('.expected') }.sort
    listed.each { |n| assert(names.include?(n), "conformance-controls/#{n}.expected has no sabotage control") }
    names.each { |n| assert(listed.include?(n), "control #{n} has no .expected list") }
    C::CONTROLS.each do |c|
      subtest(c.name) do
        problems = C.run_control(c)
        assert_equal([], problems, "control #{c.name} (#{c.rule})")
        puts "control #{c.name}: exactly the #{C.read_expected(c.name).size} expected vectors failed"
      end
    end
  end

  def test_control_needle_must_match
    bogus = C::Control.new('no-such-needle', 'none',
                           [C.patch([C::READ], 'this text appears nowhere in the source', '')])
    assert_raises(C::RunnerError, /needle no longer matches/) { C.load_sabotaged(bogus) }
    twice = C::Control.new('twice', 'none', [C.patch([C::READ, 'def serialize_int64('], 'span', '')])
    assert_raises(C::RunnerError, /needle matches more than once/) { C.load_sabotaged(twice) }
  end

  def test_a_sabotaged_library_leaves_the_real_one_untouched
    C.load_sabotaged(C::CONTROLS.find { |c| c.name == 'float-finite' })
    rs = Serialize::ReadStream.new("\x00\x00\x80\x7F".b)
    refute(rs.serialize_float(Serialize::Ref.new))
    assert_equal(:non_finite_float, rs.refusal)
  end

  def test_runner_witnesses
    C::WITNESSES.each do |good, bad, why|
      subtest(why) do
        C.run_record(C.parse_vectors_text(good, 'witness')[0])
        assert_raises(C::RunnerError) { C.run_record(C.parse_vectors_text(bad, 'witness')[0]) }
      end
    end
    ["operation bool\nname m\nbytes\nexpect refused\n",
     "operation bool\nname m\nbytes\nexpect refused overflow\n",
     "operation bool\nname m\nbytes\nexpect refused past_end\nfrobnicate 1\n"].each do |text|
      subtest(text.inspect) { assert_raises(C::RunnerError) { C.parse_vectors_text(text, 'malformed') } }
    end
    C.parse_vectors_text("operation bool\nname m\nbytes\nexpect refused past_end\n", 'ok')
  end

  def test_corpus_root_override
    assert_equal(C::ROOT, C.corpus_root_of(nil))
    assert_equal(C::ROOT, C.corpus_root_of(''))
    assert_equal('x', C.corpus_root_of('x'))
    assert_raises(C::RunnerError, /is missing or empty/) { C.run_corpus(File.join(C::ROOT, 'no-such-corpus-root')) }
  end
end
