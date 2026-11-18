# frozen_string_literal: true

# The tables bench: the Ruby runner (docs/PORT-RUBY.md; bench/BENCH-STANDARD.md).
#
# THIS FILE IS SHAPE-BLIND. It names the generated root once, in ROOT below,
# and nothing else: no field, no pinned value, no wire size.

MAX_RUNS = 7
NUM_VARIANTS = 64
DEFAULT_ITERATIONS = 640
ROOT = 'TableMixed'

module RubyTableBench
  @failed = false
  @goldens = {}
  @csv_rows = []

  def self.fail_case(name, what)
    $stderr.puts("FAILED: #{name}: #{what}")
    @failed = true
  end

  def self.usage
    $stderr.puts('usage: table_main.rb <generated dir> [--indexed] [--gate] [--csv] [--round K] ' \
                 '[--iterations N] [--wire-dir D] [--variant-dir D]')
    exit(1)
  end

  def self.read_file(directory, basename)
    path = File.join(directory, basename)
    unless File.file?(path)
      $stderr.puts("missing corpus #{path} -- run from the schema repo root (or pass --wire-dir/--variant-dir)")
      return nil
    end
    data = File.binread(path)
    @goldens[basename] = data
    data
  end

  def self.corpus_id
    h = 0xcbf29ce484222325
    @goldens.keys.sort.each do |name|
      [name.b, "\x00".b, @goldens[name]].each do |chunk|
        chunk.each_byte do |byte|
          h = ((h ^ byte) * 0x100000001b3) & 0xFFFFFFFFFFFFFFFF
        end
      end
    end
    format('%016x', h)
  end

  def self.stats(rates)
    sorted = rates.sort
    median = sorted[sorted.length / 2]
    {
      median: median,
      min: sorted[0],
      max: sorted[-1],
      spread: ((sorted[-1] - sorted[0]) / median) * 100.0
    }
  end

  def self.report(bench, path, iters, bytes_per_op, stat, csv, num_runs)
    mbps = stat[:median] * bytes_per_op / (1024.0 * 1024.0)
    $stderr.printf("%-18s %-11s %10.4f M msg/s %10.2f MB/s   (min %.4f, max %.4f, spread %.1f%%)\n",
                   bench, path, stat[:median] / 1e6, mbps, stat[:min] / 1e6, stat[:max] / 1e6, stat[:spread])
    return unless csv

    @csv_rows << format('ruby,%s,%s,%d,%.17g,%d,%.0f,%.0f,%.0f,%.2f,%.2f',
                        bench, path, iters, bytes_per_op, num_runs,
                        stat[:median], stat[:min], stat[:max], mbps, stat[:spread])
  end

  def self.flush_csv(csv)
    return unless csv

    if @failed
      $stderr.puts('refusing to emit CSV rows from a failing run')
      return
    end
    $stdout.puts('lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,' \
                 'median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline')
    ident = corpus_id
    @csv_rows.each do |row|
      $stdout.puts("#{row},#{ident},table,module,contract,default,unknown")
    end
  end

  def self.load_variants(name, variant_dir, indexed)
    packed = read_file(variant_dir, "#{name}.variants.bin")
    return nil if packed.nil?

    lengths = if indexed
                idx_bytes = read_file(variant_dir, "#{name}.lengths")
                if idx_bytes.nil? || idx_bytes.bytesize != NUM_VARIANTS * 4
                  fail_case(name, 'the length index is not 64 little-endian uint32 lengths')
                  return nil
                end
                idx_bytes.unpack("V#{NUM_VARIANTS}")
              else
                if packed.empty? || (packed.bytesize % NUM_VARIANTS) != 0
                  fail_case(name, 'the unindexed corpus is not 64 equal records')
                  return nil
                end
                Array.new(NUM_VARIANTS, packed.bytesize / NUM_VARIANTS)
              end
    variants = []
    offset = 0
    lengths.each_with_index do |size, k|
      if size <= 0 || offset + size > packed.bytesize
        fail_case(name, "variant #{k}'s length does not fit the corpus")
        return nil
      end
      variants << packed.byteslice(offset, size)
      offset += size
    end
    if offset != packed.bytesize
      fail_case(name, 'the lengths do not sum to the corpus size')
      return nil
    end
    [variants, packed.bytesize.to_f / NUM_VARIANTS]
  end

  def self.import_generated(generated)
    entries = Dir.children(generated).sort.select do |n|
      !n.start_with?('_') && n.end_with?('.rb') && File.directory?(File.join(generated, n.delete_suffix('.rb')))
    end
    if entries.length != 1
      $stderr.puts("expected one generated package under #{generated}")
      exit(1)
    end
    pkg = entries[0].delete_suffix('.rb')
    before = Object.constants
    require File.expand_path(File.join(generated, entries[0]))
    added = Object.constants - before
    mod_name = added.first || pkg.split('_').map(&:capitalize).join.to_sym
    Object.const_get(mod_name, false)
  end

  def self.run_bench(mod, iters, csv, gate, indexed, num_runs, wire_dir, variant_dir)
    name = 'bench_table'
    cls = mod.const_get(ROOT, false)
    refused_cls = mod::Refused
    invalid_cls = mod::InvalidValue

    loaded = load_variants(name, variant_dir, indexed)
    return if loaded.nil?

    variants, bytes_per_op = loaded
    unless indexed
      golden = read_file(wire_dir, "#{name}.bin")
      return if golden.nil?

      if variants[0] != golden
        $stderr.puts("WIRE GOLDEN MISMATCH on #{name}: variant 0 (#{variants[0].bytesize} B) != #{wire_dir}/#{name}.bin (#{golden.bytesize} B)")
        @failed = true
        return
      end
    end

    decoded = []
    variants.each_with_index do |wire, k|
      begin
        rep = mod::LoadReport.new
        value = cls.load(wire, rep)
      rescue refused_cls => e
        fail_case(name, "load refused variant #{k}: #{e.reason}")
        return
      end
      if rep.unknown != 0 || rep.widened != 0
        fail_case(name, "variant #{k} reported unknown or widened fields")
        return
      end
      begin
        written = value.save
      rescue invalid_cls => e
        fail_case(name, "save refused variant #{k}: #{e.path}: #{e.rule}")
        return
      end
      if written != wire
        fail_case(name, "round-trip wire mismatch on variant #{k}")
        return
      end
      decoded << value
    end
    return if gate

    clock = -> { Process.clock_gettime(Process::CLOCK_PROCESS_CPUTIME_ID) }

    # Warmup + measured write
    write_rates = []
    (num_runs + 1).times do |r|
      t0 = clock.call
      i = 0
      while i < iters
        decoded[i & 63].save
        i += 1
      end
      dt = clock.call - t0
      write_rates << (iters / dt) if r.positive?
    end
    w_stat = stats(write_rates)
    report(name, 'write', iters, bytes_per_op, w_stat, csv, num_runs)

    # Warmup + measured round-trip
    rt_rates = []
    (num_runs + 1).times do |r|
      t0 = clock.call
      i = 0
      while i < iters
        v = cls.load(variants[i & 63])
        v.save
        i += 1
      end
      dt = clock.call - t0
      rt_rates << (iters / dt) if r.positive?
    end
    rt_stat = stats(rt_rates)
    report(name, 'round_trip', iters, bytes_per_op, rt_stat, csv, num_runs)

    read_time = (1.0 / rt_stat[:median]) - (1.0 / w_stat[:median])
    return unless read_time.positive?

    $stderr.printf("%-18s %-11s %10.4f M msg/s   (DERIVED: round-trip minus write, informational -- not a measured row)\n",
                   name, 'read', 1e-6 / read_time)
  end

  def self.main
    args = ARGV.dup
    usage if args.empty? || args[0].start_with?('--')
    generated = args.shift
    csv = false
    gate = false
    indexed = false
    iters = DEFAULT_ITERATIONS
    num_runs = MAX_RUNS
    wire_dir = 'testdata/wire'
    variant_dir = 'bench/corpus/variants'

    until args.empty?
      a = args.shift
      case a
      when '--indexed' then indexed = true
      when '--gate' then gate = true
      when '--csv' then csv = true
      when '--round'
        usage if args.empty?
        args.shift
        num_runs = 1
      when '--iterations'
        usage if args.empty?
        iters = Integer(args.shift)
        usage if iters <= 0
      when '--wire-dir'
        usage if args.empty?
        wire_dir = args.shift
      when '--variant-dir'
        usage if args.empty?
        variant_dir = args.shift
      else
        usage
      end
    end

    $stderr.puts("Ruby #{RUBY_VERSION} (#{RUBY_PLATFORM}) -- #{iters} msgs/run, median of #{num_runs}") unless gate
    mod = import_generated(generated)
    run_bench(mod, iters, csv, gate, indexed, num_runs, wire_dir, variant_dir)
    flush_csv(csv)
    exit(@failed ? 1 : 0)
  end
end

RubyTableBench.main
