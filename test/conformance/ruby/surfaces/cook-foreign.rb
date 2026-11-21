# frozen_string_literal: true

require_relative '_blocks'
require_relative '_cooks'

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'cook').each do |row|
    name, unit, root, path = row[1, 4]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.no_codec(outdir, name)
      next
    end
    handle = RubyConformanceCooks.open_cook(pkg, root, RubyConformanceBlocks.foreign(RubyConformance.read_bytes(path)))
    RubyConformance.write_bytes(outdir, name, handle ? "open\n".b : "refuse\n".b)
  end
end
