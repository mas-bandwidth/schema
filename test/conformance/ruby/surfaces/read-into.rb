# frozen_string_literal: true

# READ INTO THE CALLER'S VALUES (SPEC-TABLES §6.5, §3.4; README, the
# 2026-11-22 surfaces): one report and one set of caller values through a
# lawful read, a refused read and the lawful read again.
#
# The Ruby reads (Load, FromJson, FixedLoad, LoadMessages) are handed the
# report and RETURN new values; the caller's values are what it assigns from a
# read that returns. A Ruby read refuses by raising Refused, which this driver
# answers `threw` (the README: a read refuses through its report and never by
# throwing, FL-29). The driver catches only to say so.
def self.attempt(pkg, values)
  result = yield
  values.replace(result.is_a?(::Array) ? result : [result])
  "read\n"
rescue pkg::Refused => e
  warn "read-into: the read raised #{e.class}: #{e.message}"
  "threw\n"
end

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'read-into').each do |row|
    case_name, unit, root, form, lawful, damaged, count, announcement = row[1, 8]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.absent(outdir, case_name, 'unit not generated for ruby')
      next
    end
    if form == 'fixed-nocap'
      # FL-17: the fixed load with NO stated capacity; the answer is the
      # record count it returned
      report = pkg::FixedLoadReport.new
      values = []
      line = attempt(pkg, values) { cls.fixed_load(RubyConformance.read_bytes(lawful), report: report) }
      out = line == "read\n" ? "read\n#{values.length}\n" : line
      RubyConformance.write_bytes(outdir, case_name, out.b)
      next
    end
    n = Integer(count, 10)
    read =
      case form
      when 'variable'
        report = pkg::LoadReport.new
        ->(path) { cls.load(RubyConformance.read_bytes(path), report) }
      when 'json'
        report = pkg::LoadReport.new
        ->(path) { cls.from_json(RubyConformance.read_utf8_raw(path), report) }
      when 'fixed'
        report = pkg::FixedLoadReport.new
        ->(path) { cls.fixed_load(RubyConformance.read_bytes(path), n, report) }
      when 'message'
        report = pkg::LoadReport.new
        vocab = pkg::MessageVocabulary.new
        pkg.announce_read(vocab, RubyConformance.read_bytes(announcement))
        ->(path) { pkg.load_messages(cls, vocab, RubyConformance.read_bytes(path), n, report) }
      else
        raise "#{case_name}: unknown read-into form #{form}"
      end
    values = Array.new(n) { cls.new }
    out = String.new(encoding: ::Encoding::BINARY)
    out << attempt(pkg, values) { read.call(lawful) }
    out << attempt(pkg, values) { read.call(damaged) }
    values.each { |v| out << v.to_json.b }
    out << attempt(pkg, values) { read.call(lawful) }
    RubyConformance.write_bytes(outdir, case_name, out)
  end
end
