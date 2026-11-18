# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'instance').each do |row|
    case_name, unit, root, wire = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.supported?(pkg, cls)
      RubyConformance.absent(outdir, case_name, 'wire root is not generated and supported')
      next
    end
    begin
      value = cls.load(RubyConformance.read_bytes(wire))
    rescue pkg::Refused => e
      raise "#{case_name}: unexpected public wire refusal: #{e.reason}" unless e.reason == 'blob_shared'

      RubyConformance.absent(outdir, case_name, 'blob_shared')
      next
    end
    RubyConformance.write_bytes(outdir, case_name, value.save)
  end
end
