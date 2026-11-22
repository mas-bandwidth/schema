# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'instance').each do |row|
    case_name, unit, root = row[1, 3]
    next if row.length > 5 && row[5] == 'no-text'

    pkg = RubyConformance.unit_package(unit)
    cls = RubyConformance.root_class(unit, root)
    if pkg.nil? || cls.nil? || !RubyConformance.fixed_carried?(pkg, cls)
      RubyConformance.no_codec(outdir, case_name)
      next
    end
    text = RubyConformance.read_utf8_raw("testdata/conformance/tables/json/#{case_name}.json")
    RubyConformance.write_bytes(outdir, case_name, cls.from_json(text).save)
  end
end
