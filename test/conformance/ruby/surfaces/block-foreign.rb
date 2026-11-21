# frozen_string_literal: true

require_relative '_blocks'

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'block').each do |row|
    name, unit, path = row[1], row[2], row[3]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.no_codec(outdir, name)
      next
    end
    root = RubyConformanceBlocks.block_root(name)
    if root.nil?
      RubyConformance.no_codec(outdir, name)
      next
    end
    handle = RubyConformanceBlocks.open_block(pkg, root, RubyConformanceBlocks.foreign(RubyConformance.read_bytes(path)))
    RubyConformance.write_bytes(outdir, name, handle ? "open\n".b : "refuse\n".b)
  end
end
