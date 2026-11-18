# frozen_string_literal: true

# Golden-gated Ruby packet bench over the committed 64-record corpus.
require "optparse"
require File.expand_path("generated/bench/ruby/bench")

def corpus_id(files)
  h = 0xCBF29CE484222325
  files.keys.sort.each do |name|
    blob = "#{name}\0".b + files[name]
    blob.each_byte do |b|
      h = ((h ^ b) * 0x100000001B3) & 0xFFFFFFFFFFFFFFFF
    end
  end
  format("%016x", h)
end

def median(arr)
  s = arr.sort
  n = s.length
  (n.odd? ? s[n / 2] : (s[(n / 2) - 1] + s[n / 2]) / 2.0)
end

csv = false
gate = false
quick = false
round_num = nil
iterations = nil
wire_dir = "testdata/wire"
variant_dir = "bench/corpus/variants"

OptionParser.new do |opts|
  opts.on("--csv") { csv = true }
  opts.on("--gate") { gate = true }
  opts.on("--quick") { quick = true }
  opts.on("--round N", Integer) { |v| round_num = v }
  opts.on("--iterations N", Integer) { |v| iterations = v }
  opts.on("--wire-dir DIR") { |v| wire_dir = v }
  opts.on("--variant-dir DIR") { |v| variant_dir = v }
end.parse!(ARGV)

abort("--round must be nonnegative") if !round_num.nil? && round_num < 0
iters = iterations || (quick ? 1280 : 12_800)
abort("--iterations must be a positive multiple of 64 up to 2147483584") if iters <= 0 || iters > 2_147_483_584 || (iters % 64) != 0

golden = File.binread(File.join(wire_dir, "bench_mixed.bin"))
packed = File.binread(File.join(variant_dir, "bench_mixed.variants.bin"))
raise ArgumentError, "invalid variant corpus" if golden.empty? || packed.empty? || (packed.bytesize % 64) != 0

size = packed.bytesize / 64
variants = Array.new(64) { |i| packed.byteslice(i * size, size) }
raise ArgumentError, "variant 0 differs from the C++ pin" if variants[0] != golden

values = variants.map { |data| Bench::BenchMixed.read(data) }
values.each_with_index do |value, i|
  raise ArgumentError, "variant #{i} round-trip differs" if value.write != variants[i]
end

identifier = corpus_id({ "bench_mixed.bin" => golden, "bench_mixed.variants.bin" => packed })
if gate
  $stderr.puts("OK (gate only, corpus_id #{identifier})")
  exit 0
end

runs = !round_num.nil? ? 1 : (quick ? 3 : 7)
rows = []
sink = 0
yjit_on = defined?(RubyVM::YJIT) && RubyVM::YJIT.enabled?
opt_label = yjit_on ? "yjit" : "interp"

%w[write round_trip].each do |path|
  rates = []
  (runs + 1).times do |r|
    t0 = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    i = 0
    while i < iters
      idx = i & 63
      value = path == "write" ? values[idx] : Bench::BenchMixed.read(variants[idx])
      sink += value.write.bytesize
      i += 1
    end
    elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC) - t0
    rates << (iters / elapsed) if r > 0
  end
  med = median(rates)
  lo = rates.min
  hi = rates.max
  mb = med * size / 1_048_576.0
  spread = (hi - lo) / med * 100.0
  rows << format("ruby,bench_mixed,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f,%s,gen,pkg,always,%s,unknown",
                 path, iters, size, runs, med, lo, hi, mb, spread, identifier, opt_label)
end

if csv
  puts "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline"
  puts rows.join("\n")
else
  $stderr.puts rows.join("\n")
end
$stderr.puts "sink: #{sink}"
