# frozen_string_literal: true

# The Ruby driver of the packet UTF-8 sweep (test/packet-text/README.md):
# one hexadecimal wire per input line ("-" is the empty wire), one verdict per
# output line, "REFUSE" or "OK <bits> <payload> <bits> <canonical>".
require File.expand_path("packettext", ARGV.fetch(0))

out = []
$stdin.read.split.each do |line|
  wire = line == "-" ? "".b : [line].pack("H*")
  value = Packettext::Narrow.new
  r = Packettext::ReadStream.new(wire)
  unless value._s_read(r) && r.serialize_end
    out << "REFUSE"
    next
  end
  w = Packettext::WriteStream.new(256)
  begin
    value._s_validate("Narrow")
    value._s_write(w)
  rescue Packettext::InvalidValue => e
    out << "WRITE-REFUSED #{e.class.name}"
    next
  end
  written = w.bits_processed
  w.flush
  data = w.data
  payload_hex = value.text.b.unpack1("H*")
  payload_hex = "-" if payload_hex.empty?
  canon_hex = data.unpack1("H*")
  canon_hex = "-" if canon_hex.empty?
  out << format("OK %d %s %d %s", r.bits_processed, payload_hex, written, canon_hex)
end
$stdout.write("#{out.join("\n")}\n")
