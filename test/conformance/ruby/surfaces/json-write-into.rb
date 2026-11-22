# frozen_string_literal: true

# FL-14, FL-15 (SPEC-TABLES §16.1; README, the 2026-11-22 surfaces). Ruby has
# no process-locale call in its core library (no setlocale): no Ruby code can
# make SCHEMA_COMMA_LOCALE the process's locale, and Float formatting never
# reads one. ToJson returns a new String rather than filling a caller buffer,
# so there is no tail for the writer to touch. The driver says so here and
# runs the rest: Load the wire, ToJson, the text.
def self.run(manifest, outdir)
  raise 'json-write-into: the harness handed no SCHEMA_COMMA_LOCALE' if ENV.fetch('SCHEMA_COMMA_LOCALE', '').empty?

  RubyConformance.entries(manifest, 'json-write-into').each do |row|
    case_name, unit, root, wire = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.absent(outdir, case_name, 'unit not generated for ruby')
      next
    end
    value = cls.load(RubyConformance.read_bytes(wire))
    RubyConformance.write_bytes(outdir, case_name, value.to_json.b)
  end
end
