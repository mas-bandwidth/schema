# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.cases(manifest, 'cook') do |row|
    name, unit, root, path = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.no_codec(outdir, name)
      next
    end
    cls = pkg.const_get(root, false)
    handle = pkg::SchemaTable.open_cook(cls, RubyConformance.read_bytes(path))
    RubyConformance.write_bytes(outdir, name, pkg::SchemaTable.dump_cook(handle).b)
  end
end
