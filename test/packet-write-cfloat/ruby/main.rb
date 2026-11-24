# frozen_string_literal: true

# The Ruby packet Write of a compressed float (SPEC §5.1 row 10,
# PACKET-WIRE-ALGORITHM.md §2.5) over the shared cases in
# test/packet-write-cfloat/cases.txt.
#
# usage: ruby main.rb <generated dir> <cases.txt>
#
# Ruby is an every-build leg: a refused value raises InvalidValue from the
# writer in every build. A written case writes, reads back to its quantized
# value (or the case's reads-back value), and, when the case pins a wire,
# writes exactly those bytes. The last stdout line is the count
# tools/mustrun reads.
require File.expand_path("cfloat", ARGV.fetch(0))

$failures = 0
ran = 0

def fail_case(name, detail)
  $stderr.write("FAILED: #{name}: #{detail}\n")
  $failures += 1
end

def case_value(text)
  case text
  when "NaN" then Float::NAN
  when "+Inf" then Float::INFINITY
  when "-Inf" then -Float::INFINITY
  else Float(text)
  end
end

def bits32(v) = [v].pack("e").unpack1("L<")

File.readlines(ARGV.fetch(1)).each do |raw|
  fields = raw.split
  next if fields.empty? || fields[0].start_with?("#")

  ran += 1
  name = fields[0]
  if fields.length < 3 || fields.length.even? || !%w[refused written].include?(fields[2])
    fail_case(name, "malformed case #{raw.strip.inspect}")
    next
  end
  x = case_value(fields[1])
  verdict = fields[2]
  # [reads-back <v>] [type RatioEndpoint] [wire <hex>], in pairs
  want = x
  ratio = false
  wire_hex = nil
  malformed = false
  fields[3..].each_slice(2) do |key, operand|
    if key == "reads-back" then want = case_value(operand)
    elsif key == "type" && operand == "RatioEndpoint" then ratio = true
    elsif key == "wire" then wire_hex = operand.downcase
    else malformed = true
    end
  end
  if malformed
    fail_case(name, "malformed modifier in #{raw.strip.inspect}")
    next
  end
  input = ratio ? Cfloat::RatioEndpoint.new : Cfloat::CFloat.new
  if ratio then input.ratio = x else input.x = x end
  begin
    wire = input.write
  rescue Cfloat::InvalidValue
    fail_case(name, "the writer refused a lawful value") if verdict != "refused"
    next
  rescue StandardError => e
    fail_case(name, "the writer raised #{e.class}: #{e.message}")
    next
  end
  if verdict == "refused"
    fail_case(name, "the writer accepted a value the range refuses (#{wire.unpack1("H*")})")
    next
  end
  if wire_hex && wire.unpack1("H*") != wire_hex
    fail_case(name, "wrote #{wire.unpack1("H*")}, want #{wire_hex}")
    next
  end
  begin
    got = ratio ? Cfloat::RatioEndpoint.read(wire).ratio : Cfloat::CFloat.read(wire).x
  rescue Cfloat::Refused => e
    fail_case(name, "the reader refused the writer's bytes: #{e.message}")
    next
  end
  fail_case(name, "read back #{got.inspect}, want #{want.inspect}") if bits32(got) != bits32(want)
end

puts "packet-write-cfloat Ruby: ran #{ran}, failed #{$failures}"
exit 1 if $failures != 0 || ran.zero?
