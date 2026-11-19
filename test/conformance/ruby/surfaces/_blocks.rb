# frozen_string_literal: true

module RubyConformanceBlocks
  BLOCK_ROOTS = {
    'block_render' => 'RenderFrame',
    'block_padded' => 'PaddedFrame',
    'block_torture' => 'TortureFrame'
  }.freeze

  def self.block_root(name)
    BLOCK_ROOTS.each do |prefix, root|
      return root if name.start_with?(prefix)
    end
    nil
  end

  def self.open_block(pkg, root_name, data, base_offset = 0)
    cls = pkg.const_get(root_name, false)
    pkg::SchemaTable.block_open(cls, data, build_version: pkg::BUILD_VERSION, base_offset: base_offset)
  rescue StandardError => e
    raise unless e.is_a?(pkg::Refused)

    nil
  end

  def self.open_raw(pkg, root_name, data)
    cls = pkg.const_get(root_name, false)
    handle = pkg::SchemaTable.block_open(cls, data, build_version: pkg::BUILD_VERSION)
    [handle, -> { pkg::SchemaTable.block_dump(handle) }]
  end

  def self.foreign(source)
    return source if source.bytesize < 8

    head = (source.byteslice(0, 8) || ''.b).reverse
    tail = source.byteslice(8, source.bytesize - 8) || ''.b
    head + tail
  end

  def self.place(source, extent)
    return source if extent.negative? || extent == source.bytesize
    return source.byteslice(0, extent) || ''.b if extent < source.bytesize

    source + ("\x00".b * (extent - source.bytesize))
  end
end
