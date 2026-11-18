# frozen_string_literal: true

# The fuzz harness of serialize.rb: the nightly tier the family carries.
#
# Two properties, both deterministic from a seed (STANDARD.md, "Reader
# Obligations"; IMPLEMENTING.md U5):
#
#   (a) a round trip: random sequences of operations with random lawful values
#       write bytes that read back to the same values, and whose canonical
#       rewrite is exactly those bytes;
#   (b) a read: random and mutated byte strings through random operation
#       sequences never raise, and every read is either accepted with a
#       byte-identical canonical rewrite or refused with a reason from the
#       closed list.
#
# A fixed set of crafted cases (inputs with a stated outcome) runs with every
# seed, so the negative control has a deterministic red, by name.
#
# Modes
#
#   ruby -w fuzz/fuzz.rb --replay DIR [--lib DIR]
#       Replays the saved corpus: every file in DIR holds one seed (an
#       integer), and each seed runs its fixed set of cases. Deterministic, a
#       few seconds, no clock: the per-commit gate. Exits 1 on any failure.
#
#   ruby -w fuzz/fuzz.rb --seed N (--iterations N | --timeout S) [--lib DIR]
#       A live run from seed N: exactly N cases, or as many as fit S seconds
#       (the nightly run). The budget is an argument; a run's cases depend only
#       on the seed, so a failure at case K replays with --iterations K+1.
#
# --lib names the directory holding serialize.rb (default: this repository's
# lib/). The negative control points it at a copy with a planted defect. The
# harness uses the standard library only and is silent under ruby -w: a run in
# which Ruby printed a warning is red.

# The harness: generators, the read and write drivers, and the two modes.
module Fuzz
  ROOT = File.dirname(__dir__)

  # every warning Ruby emits, so a run that printed one is red
  module WarningTrap
    CAUGHT = [] # rubocop:disable Style/MutableConstant -- the run's warnings

    def warn(message, *, **)
      CAUGHT << message
      super
    end
  end
  Warning.singleton_class.prepend(WarningTrap)

  RELATIVE_MAX = (1 << 31) - 1

  # Round trips, mutated reads and random reads each seed runs on replay.
  ROUNDTRIP = 400
  MUTATE = 400
  RANDOM_READ = 400

  # A value a read did not store.
  UNSET = Object.new.freeze

  # One operation: a name, its declaration arguments, the value it carries,
  # and, for object, its nested operations.
  Op = Struct.new(:name, :args, :value, :children) do
    def self.of(name, args = [], value = nil, children = nil)
      new(name, args, value, children)
    end
  end

  CHARS = ((0x20..0x7E).map(&:chr) + %w[é € 中 😀]).freeze

  module_function

  # ---------------------------------------------------------------------------
  # generators: every value lawful for its declaration
  # ---------------------------------------------------------------------------

  def bits_of(rng, n)
    rng.rand(1 << n)
  end

  def gen_string(rng, max_bytes)
    chars = Array.new(rng.rand(0..8)) { CHARS[rng.rand(CHARS.size)] }
    chars.pop while chars.join.bytesize > max_bytes
    chars.join.encode(Encoding::UTF_8)
  end

  def gen_fixed(rng)
    width = [8, 16, 32, 64, 128][rng.rand(5)]
    ib = rng.rand(1..width)
    fb = width - ib
    signed = ib > 1 && rng.rand(2).zero?
    cap = signed ? (1 << (ib - 1)) - 1 : (1 << ib) - 1
    cap = [cap, (1 << 63) - 1].min
    lo = signed ? rng.rand(-cap..-1) : rng.rand(0..cap)
    hi = rng.rand(lo..cap)
    raw_min = lo << fb
    raw_span = (hi - lo) << fb
    Op.of(:fixed, [ib, fb, lo, hi], raw_min + rng.rand(0..raw_span))
  end

  def gen_compressed(rng, s)
    200.times do
      mn = rng.rand(-5000..4999).to_f
      mx = mn + rng.rand(1..10_000)
      res = [0.01, 0.1, 1.0, 10.0, 100.0, 0.5, 2.0][rng.rand(7)]
      return Op.of(:compressed_float, [mn, mx, res], rng.rand(-1e4..1e4)) if s.compressed_float_params(mn, mx, res)
    end
    raise 'could not generate a compressed_float declaration'
  end

  def gen_ranged(rng, name, width)
    lo = rng.rand(-(1 << (width - 1))..((1 << (width - 1)) - 1))
    hi = rng.rand(lo..((1 << (width - 1)) - 1))
    Op.of(name, [lo, hi], rng.rand(lo..hi))
  end

  def gen_object(rng, depth, s)
    Op.of(:object, [], nil, Array.new(rng.rand(1..4)) { gen_op(rng, depth + 1, s) })
  end

  def gen_op(rng, depth, s)
    roll = rng.rand(100)
    return gen_object(rng, depth, s) if depth < 2 && roll < 5

    case roll
    when 0...20
      w = rng.rand(1..64)
      Op.of(:bits, [w], bits_of(rng, w))
    when 20...27 then Op.of(:bool, [], rng.rand(2).zero?)
    when 27...32 then Op.of(:align)
    when 32...40
      bits = (bits_of(rng, 1) << 31) | (rng.rand(0..254) << 23) | bits_of(rng, 23)
      Op.of(:float, [], s.float32_from_bits(bits))
    when 40...48
      bits = (bits_of(rng, 1) << 63) | (rng.rand(0..2046) << 52) | bits_of(rng, 52)
      Op.of(:double, [], s.float64_from_bits(bits))
    when 48...56 then gen_ranged(rng, :int, 32)
    when 56...62 then gen_ranged(rng, :int64, 64)
    when 62...68 then gen_ranged(rng, :int128, 128)
    when 68...73 then Op.of(:uint128, [], bits_of(rng, 128))
    when 73...82 then gen_fixed(rng)
    when 82...88
      previous = rng.rand(0..(RELATIVE_MAX - 1))
      Op.of(:int_relative, [previous], rng.rand((previous + 1)..RELATIVE_MAX))
    when 88...94 then gen_compressed(rng, s)
    when 94...97
      count = rng.rand(0..40)
      Op.of(:bytes, [count], rng.bytes(count))
    else
      buffer_size = rng.rand(1..64)
      Op.of(:string, [buffer_size], gen_string(rng, buffer_size - 1))
    end
  end

  def gen_ops(rng, s)
    Array.new(rng.rand(1..6)) { gen_op(rng, 0, s) } << Op.of(:end)
  end

  # ---------------------------------------------------------------------------
  # drivers: one call per operation, the same for a writer, a reader and a
  # measure
  # ---------------------------------------------------------------------------

  def call(stream, op, ref)
    a = op.args
    case op.name
    when :bits then stream.serialize_bits(ref, a[0])
    when :bool then stream.serialize_bool(ref)
    when :align then stream.serialize_align
    when :float then stream.serialize_float(ref)
    when :double then stream.serialize_double(ref)
    when :int then stream.serialize_int(ref, a[0], a[1])
    when :int64 then stream.serialize_int64(ref, a[0], a[1])
    when :int128 then stream.serialize_int128(ref, a[0], a[1])
    when :uint128 then stream.serialize_uint128(ref)
    when :fixed then stream.serialize_fixed(ref, a[0], a[1], a[2], a[3])
    when :int_relative then stream.serialize_int_relative(a[0], ref)
    when :compressed_float then stream.serialize_compressed_float(ref, a[0], a[1], a[2])
    when :bytes then stream.serialize_bytes(ref, a[0])
    when :string then stream.serialize_string(ref, a[0])
    when :end then stream.serialize_end
    else raise "no driver for #{op.name}"
    end
  end

  def write_tree(s, stream, ops)
    ops.all? do |op|
      if op.name == :object
        stream.serialize_object { |inner| write_tree(s, inner, op.children) }
      else
        call(stream, op, s::Ref.new(op.value))
      end
    end
  end

  # [ok, the operations as read]; every operation is driven, so a refusal
  # mid-sequence also drives the latched reads after it
  def read_tree(s, stream, ops)
    all_ok = true
    back = ops.map do |op|
      ok, node = read_op(s, stream, op)
      all_ok &&= ok
      node
    end
    [all_ok, back]
  end

  def read_op(s, stream, op)
    if op.name == :object
      node = Op.of(:object, op.args, nil, [])
      ok = stream.serialize_object do |inner|
        inner_ok, node.children = read_tree(s, inner, op.children)
        inner_ok
      end
      return [ok, node]
    end
    ref = s::Ref.new(UNSET)
    ok = call(stream, op, ref)
    [ok, Op.of(op.name, op.args, ok ? ref.value : UNSET)]
  end

  def flatten(ops)
    ops.flat_map { |op| op.name == :object ? flatten(op.children) : [op] }
  end

  # a write buffer holding bits, a multiple of 8 bytes with room to spare
  def capacity_for(bits)
    (((bits + 63) / 64) * 8) + 8
  end

  def write_packet(s, ops)
    measure = s::MeasureStream.new
    raise 'the measure refused a lawful write' unless write_tree(s, measure, ops)

    writer = s::WriteStream.new(capacity_for(measure.bits_processed))
    raise 'the write refused a lawful value' unless write_tree(s, writer, ops)

    writer.flush
    writer.data
  end

  # nil when the operations as read write back to exactly data
  def writeback(s, back, data)
    again = write_packet(s, back)
    return nil if again.b == data.b

    "accepted read: the canonical rewrite does not reproduce the bytes (#{hex(data)} became #{hex(again)})"
  rescue StandardError => e
    "accepted read: the canonical rewrite raised #{e.class}: #{e.message}"
  end

  def hex(data)
    data.unpack1('H*')
  end

  # read_tree's [ok, back], or [nil, why] when the read raised
  def read_or_raised(s, stream, ops)
    read_tree(s, stream, ops)
  rescue StandardError, SystemStackError => e
    [nil, "read raised #{e.class}: #{e.message}"]
  end

  # a read of data never raises, and is accepted with a byte-identical
  # rewrite or refused with a reason from the closed list
  def check_read(s, ops, data)
    stream = s::ReadStream.new(data)
    ok, back = read_or_raised(s, stream, ops)
    return back if ok.nil?
    return writeback(s, back, data) if ok
    return nil if s::REASONS.include?(stream.refusal)

    "refused with a reason outside the closed list: #{stream.refusal.inspect}"
  end

  def same_value?(s, a, b)
    case a.name
    when :compressed_float, :align, :end then true
    when :float then s.float32_bits(a.value) == s.float32_bits(b.value)
    when :double then s.float64_bits(a.value) == s.float64_bits(b.value)
    else a.value == b.value
    end
  end

  def round_trip(rng, s)
    ops = gen_ops(rng, s)
    data = write_packet(s, ops)
    stream = s::ReadStream.new(data)
    ok, back = read_tree(s, stream, ops)
    return "round trip refused as #{s.refusal_name(stream.refusal)} (#{hex(data)})" unless ok

    flatten(ops).zip(flatten(back)).each do |a, b|
      return "round trip value mismatch at #{a.name}: wrote #{a.value.inspect}, read #{b.value.inspect}" unless
        same_value?(s, a, b)
    end
    writeback(s, back, data)
  end

  def mutate(rng, data)
    return data + rng.bytes(rng.rand(1..4)) if data.empty?

    d = data.b
    i = rng.rand(d.bytesize)
    case rng.rand(6)
    when 0 then d.setbyte(i, d.getbyte(i) ^ (1 << rng.rand(8)))
    when 1 then d.setbyte(i, d.getbyte(i) ^ 0xFF)
    when 2 then d.setbyte(d.bytesize - 1, d.getbyte(d.bytesize - 1) | 1)
    when 3 then d = d.byteslice(0, rng.rand(1..d.bytesize))
    when 4 then d << rng.bytes(rng.rand(1..4))
    else
      j = rng.rand(d.bytesize)
      x = d.getbyte(i)
      d.setbyte(i, d.getbyte(j))
      d.setbyte(j, x)
    end
    d
  end

  def mutated_read(rng, s)
    ops = gen_ops(rng, s)
    check_read(s, ops, mutate(rng, write_packet(s, ops)))
  end

  def random_read(rng, s)
    ops = gen_ops(rng, s)
    check_read(s, ops, rng.bytes(rng.rand(0..64)))
  end

  # [operations, bytes, expected]: nil to accept, or the refusal's name
  def crafted
    [
      [[Op.of(:bits, [8], 0xAB), Op.of(:end)], "\xAB".b, nil],
      [[Op.of(:bits, [1], 1), Op.of(:align), Op.of(:end)], "\xFF".b, :nonzero_padding],
      [[Op.of(:bits, [3], 7), Op.of(:end)], "\x0F".b, :nonzero_padding],
      [[Op.of(:bits, [1], 1), Op.of(:bytes, [1], "\0".b), Op.of(:end)], "\x03\x00".b, :nonzero_padding],
      [[Op.of(:bits, [8], 1), Op.of(:end)], "\x01\x00".b, :trailing_data],
      [[Op.of(:bits, [8], 0), Op.of(:end)], ''.b, :past_end]
    ]
  end

  def check_crafted(s, (ops, data, expect))
    stream = s::ReadStream.new(data)
    ok, back = read_tree(s, stream, ops)
    if expect.nil?
      return writeback(s, back, data) if ok

      return "a crafted accepted case refused as #{s.refusal_name(stream.refusal)}"
    end
    return "expected refusal #{expect}, got accept" if ok
    return nil if stream.refusal == expect

    "expected refusal #{expect}, got #{s.refusal_name(stream.refusal)}"
  end

  def safe
    yield
  rescue StandardError, SystemStackError => e
    "harness exception #{e.class}: #{e.message}"
  end

  SHAPES = [
    ['round trip', ->(rng, s) { round_trip(rng, s) }],
    ['mutated read', ->(rng, s) { mutated_read(rng, s) }],
    ['random read', ->(rng, s) { random_read(rng, s) }]
  ].freeze

  def run_crafted(s, seed)
    crafted.each_with_index.filter_map do |spec, i|
      res = safe { check_crafted(s, spec) }
      "seed #{seed}: crafted case #{i}: #{res}" if res
    end
  end

  # the fixed set of cases one corpus seed runs
  def run_seed(s, seed)
    rng = Random.new(seed)
    failures = run_crafted(s, seed)
    SHAPES.zip([ROUNDTRIP, MUTATE, RANDOM_READ]).each do |(label, shape), count|
      count.times do |i|
        res = safe { shape.call(rng, s) }
        failures << "seed #{seed}: #{label} [#{i}]: #{res}" if res
      end
    end
    failures
  end

  def replay(s, directory)
    entries = Dir.exist?(directory) ? Dir.children(directory).sort : []
    if entries.empty?
      puts "FUZZ FAIL: the corpus #{directory} is missing or empty"
      return 1
    end
    failures = []
    entries.each do |entry|
      text = File.read(File.join(directory, entry)).strip
      unless text.match?(/\A[0-9]+\z/)
        failures << "corpus #{entry}: not a seed (an integer)"
        next
      end
      failures.concat(run_seed(s, Integer(text, 10)))
    end
    failures.each { |f| puts "FUZZ FAIL: #{f}" }
    cases = entries.size * (ROUNDTRIP + MUTATE + RANDOM_READ + crafted.size)
    puts "fuzz replay: #{failures.size} failures across #{entries.size} seeds, #{cases} cases"
    failures.empty? ? 0 : 1
  end

  def now
    Process.clock_gettime(Process::CLOCK_MONOTONIC)
  end

  # a live run: the shapes in turn until the budget, an iteration count or a
  # number of seconds, is spent
  def live(s, seed, iterations, timeout)
    puts "fuzz: seed #{seed}, budget #{iterations ? "#{iterations} cases" : "#{timeout}s"}"
    rng = Random.new(seed)
    failures = run_crafted(s, seed)
    deadline = timeout && (now + timeout)
    count = 0
    loop do
      break if iterations && count >= iterations
      break if deadline && now >= deadline

      label, shape = SHAPES[count % SHAPES.size]
      res = safe { shape.call(rng, s) }
      if res
        failures << "seed #{seed}: #{label} [case #{count}]: #{res}"
        puts "reproduce: <ruby> -w fuzz/fuzz.rb --seed #{seed} --iterations #{count + 1}"
      end
      count += 1
    end
    failures.each { |f| puts "FUZZ FAIL: #{f}" }
    puts "fuzz: seed #{seed}, #{count} cases, #{failures.size} failures"
    failures.empty? ? 0 : 1
  end

  def option(argv, name)
    i = argv.index(name)
    i && argv[i + 1]
  end

  def main(argv)
    lib = option(argv, '--lib') || File.join(ROOT, 'lib')
    $LOAD_PATH.unshift(File.expand_path(lib))
    require 'serialize'
    s = Object.const_get(:Serialize)
    status = run(s, argv)
    return status if WarningTrap::CAUGHT.empty?

    puts "FUZZ FAIL: Ruby printed #{WarningTrap::CAUGHT.size} warning(s) under -w:"
    WarningTrap::CAUGHT.each { |w| puts "  #{w}" }
    1
  end

  def run(s, argv)
    directory = option(argv, '--replay')
    return replay(s, directory) if directory

    seed = option(argv, '--seed')
    iterations = option(argv, '--iterations')
    timeout = option(argv, '--timeout')
    unless seed&.match?(/\A[0-9]+\z/) && (iterations || timeout)
      warn 'fuzz.rb: give --replay DIR, or --seed N with --iterations N or --timeout S'
      return 2
    end
    live(s, Integer(seed, 10), iterations && Integer(iterations, 10), timeout && Float(timeout))
  end
end

exit(Fuzz.main(ARGV)) if $PROGRAM_NAME == __FILE__
