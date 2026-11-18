# frozen_string_literal: true

require_relative '_blocks'

def self.run(manifest, outdir)
  RubyConformance.entries(manifest, 'block').each do |row|
    name, unit, path = row[1], row[2], row[3]
    pkg = RubyConformance.unit_package(unit)
    if pkg.nil?
      RubyConformance.absent(outdir, name, 'unit not generated for ruby')
      next
    end
    root = RubyConformanceBlocks.block_root(name)
    if root.nil?
      RubyConformance.absent(outdir, name, "no block root for #{name}")
      next
    end
    handle, dump = RubyConformanceBlocks.open_raw(pkg, root, RubyConformance.read_bytes(path))
    if handle.nil?
      RubyConformance.absent(outdir, name, dump)
      next
    end
    RubyConformance.write_bytes(outdir, name, dump.call.b)
  end
end
