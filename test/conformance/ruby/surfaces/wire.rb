# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'instance').each do |row|
    case_name, unit, root, wire = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    # Every instance row is a committed wire: a unit missing here is the leg
    # losing it, never a feature the port lacks (Python's wire.py raises too).
    raise "#{case_name}: wire root is not generated and supported" if pkg.nil? || cls.nil? || !RubyConformance.supported?(pkg, cls)

    begin
      value = cls.load(RubyConformance.read_bytes(wire))
    rescue pkg::Refused => e
      raise "#{case_name}: unexpected public wire refusal: #{e.reason}" unless e.reason == 'blob_shared'

      # the strict blob refusal (§2.5) is answered `refused` (ruling R13:
      # an absence is never credited on a must-refuse row)
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    RubyConformance.write_bytes(outdir, case_name, value.save)
  end
end
