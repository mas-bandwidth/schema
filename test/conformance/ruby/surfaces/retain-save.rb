# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'retain') do |row|
    case_name, unit, root, wire, capacity, ids = row[1, 6]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !pkg.const_defined?(:TableRetain, false) ||
       !cls.respond_to?(:load_retain) || !RubyConformance.supported?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    id_count = ids == 'full' ? (1 << 17) : Integer(ids, 10)
    data = RubyConformance.read_bytes(wire)
    byte_count = 1 << 20
    begin
      if capacity == 'short'
        probe = pkg::TableRetain.new("\x00".b * byte_count, "\x00".b * (24 * id_count))
        cls.load_retain(data, probe, pkg::LoadReport.new)
        byte_count = [0, probe.used - 1].max
      end
      retain = pkg::TableRetain.new("\x00".b * byte_count, "\x00".b * (24 * id_count))
      report = pkg::LoadReport.new
      value = cls.load_retain(data, retain, report)
    rescue pkg::Refused
      next
    end
    RubyConformance.write_bytes(outdir, case_name, value.save_retain(retain, report))
  end
end
