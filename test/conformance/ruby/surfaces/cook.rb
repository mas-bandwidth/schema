# frozen_string_literal: true

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'cook').each do |row|
    name, unit, root, path = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.absent(outdir, name, 'unit not generated for ruby')
      next
    end
    cls = pkg.const_get(root, false)
    handle = pkg::SchemaTable.open_cook(cls, RubyConformance.read_bytes(path))
    RubyConformance.write_bytes(outdir, name, pkg::SchemaTable.dump_cook(handle).b)
  end
end
