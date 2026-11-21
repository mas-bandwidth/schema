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
    rescue pkg::Refused => e
      # THE PUBLIC REFUSAL IS THE ANSWER, whatever its reason: the harness
      # compares the verdict (docs/rulings.json refusal-verdict-only), and
      # judges whether this input owes it
      RubyConformance.absent(outdir, case_name, e.reason)
      next
    end
    RubyConformance.write_bytes(outdir, case_name, value.save)
  end
end
