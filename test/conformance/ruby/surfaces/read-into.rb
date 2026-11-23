# frozen_string_literal: true

# READ INTO THE CALLER'S VALUES (SPEC-TABLES §6.5, §3.4; README, the
# 2026-11-22 surfaces): one report and one set of caller values through a
# lawful read, a refused read and the lawful read again.
#
# Uses load_into, from_json_into, fixed_load_into and load_messages_into
# (FL-02, FL-03, FL-29) and fixed_load with omitted capacity for fixed-nocap
# (FL-17). The driver still catches Refused only to report `threw`.
def self.attempt(pkg, report)
  ok = yield
  ok && !report.refused ? "read\n" : "refused\n"
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
      begin
        values = cls.fixed_load(RubyConformance.read_bytes(lawful), report: report)
        out = "read\n#{values.length}\n"
      rescue pkg::Refused => e
        warn "read-into: the read raised #{e.class}: #{e.message}"
        out = "threw\n"
      end
      RubyConformance.write_bytes(outdir, case_name, out.b)
      next
    end
    n = Integer(count, 10)
    values = Array.new(n) { cls.new }
    read =
      case form
      when 'variable'
        report = pkg::LoadReport.new
        ->(path) { cls.load_into(values[0], RubyConformance.read_bytes(path), report) }
      when 'json'
        report = pkg::LoadReport.new
        ->(path) { cls.from_json_into(values[0], RubyConformance.read_utf8_raw(path), report) }
      when 'fixed'
        report = pkg::FixedLoadReport.new
        ->(path) { cls.fixed_load_into(values, RubyConformance.read_bytes(path), n, report) >= 1 }
      when 'message'
        report = pkg::LoadReport.new
        vocab = pkg::MessageVocabulary.new
        pkg.announce_read(vocab, RubyConformance.read_bytes(announcement))
        ->(path) { pkg.load_messages_into(cls, values, vocab, RubyConformance.read_bytes(path), n, report) >= 1 }
      else
        raise "#{case_name}: unknown read-into form #{form}"
      end
    out = String.new(encoding: ::Encoding::BINARY)
    out << attempt(pkg, report) { read.call(lawful) }
    out << attempt(pkg, report) { read.call(damaged) }
    values.each { |v| out << v.to_json.b }
    out << attempt(pkg, report) { read.call(lawful) }
    RubyConformance.write_bytes(outdir, case_name, out)
  end
end
