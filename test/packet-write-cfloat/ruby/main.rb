# frozen_string_literal: true

require File.expand_path("cfloat", ARGV.fetch(0))

special_values = { "NaN" => Float::NAN, "+Inf" => Float::INFINITY, "-Inf" => -Float::INFINITY }
ran = 0
failed = 0
File.foreach(ARGV.fetch(1)) do |raw|
  line = raw.strip
  next if line.empty? || line.start_with?("#")

  ran += 1
  name, text, verdict, read_back, expected = line.split
  value = special_values.fetch(text) { Float(text) }
  packet = Cfloat::CFloat.new
  packet.x = value

  begin
    wire = packet.write
    if verdict != "written"
      raise "expected #{verdict}, writer accepted the value"
    end

    decoded = Cfloat::CFloat.read(wire)
    want = read_back == "reads-back" ? Float(expected) : value
    unless [decoded.x].pack("e") == [want].pack("e")
      raise "decoded #{decoded.x.inspect}, expected #{want.inspect}"
    end
  rescue Cfloat::InvalidValue => e
    if verdict != "refused" || e.path != "CFloat.x"
      warn "FAILED: #{name}: #{e.class} at #{e.path}"
      failed += 1
    end
  rescue RuntimeError => e
    warn "FAILED: #{name}: #{e.message}"
    failed += 1
  end
end

puts "packet-write-cfloat Ruby: ran #{ran}, failed #{failed}"
exit(ran.positive? && failed.zero? ? 0 : 1)
