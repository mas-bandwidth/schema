# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'instance') do |row|
    case_name, unit, root, wire = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.supported?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end

    begin
      value = cls.load(RubyConformance.read_bytes(wire))
    rescue pkg::Refused
      # The harness compares the public verdict independently of its reason.
      RubyConformance.write_bytes(outdir, case_name, "refused\n".b)
      next
    end
    RubyConformance.write_bytes(outdir, case_name, value.save)
  end
end
