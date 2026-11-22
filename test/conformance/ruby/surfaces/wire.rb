# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'instance') do |row|
    case_name, unit, root, wire = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    # Every instance row is a committed wire: a unit missing here is the leg
    # losing it, never a feature the port lacks (Python's wire.py raises too).
    raise "#{case_name}: wire root is not generated and supported" if pkg.nil? || cls.nil? || !RubyConformance.supported?(pkg, cls)

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
