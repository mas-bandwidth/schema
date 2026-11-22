# frozen_string_literal: true

module RubyConformance
  ROOT = ENV['RUBY_CONFORMANCE_GENERATED'] || 'build/conformance-ruby'
  LOADED_UNITS = {}

  def self.entries(manifest, kind)
    out = []
    File.foreach(manifest, encoding: 'UTF-8') do |raw|
      text = raw.strip
      next if text.empty? || text.start_with?('#')

      fields = text.split
      out << fields if fields[0] == kind
    end
    out
  end

  def self.read_bytes(path)
    File.binread(path)
  end

  def self.read_utf8_raw(path)
    raw = File.binread(path)
    raw.dup.force_encoding(::Encoding::UTF_8)
  end

  def self.write_bytes(outdir, name, data)
    File.binwrite(File.join(outdir, name), data)
  end

  def self.absent(outdir, name, reason)
    write_bytes(outdir, "#{name}.absent", reason.to_s.b)
  end

  # A value the leg's own storage cannot hold at all (FORMAT.md,
  # `write-refusal`): the surface answers `unrepresentable`, which the harness
  # credits on a `refused` row and no other. Raised only by the mutation,
  # never by a writer.
  class Unrepresentable < StandardError; end

  # A `<wire>` of `-` is the root's fresh value (FORMAT.md; SF-05, SF-06: a
  # root with no file form).
  def self.load_or_fresh(cls, wire)
    wire == '-' ? cls.new : cls.load(read_bytes(wire))
  end

  # `elems` (SF-34, SF-35, FL-22): the element count of a fixed `[N]T` (a
  # native Array) or a keyed `[E]T` (a KeyedArray, whose slots are the Array
  # the writers read, `_s_values`). More elements are copies of element 0,
  # fewer drop the tail. The keyed slots are resized in place: that Array is
  # the storage the writer reads, so the writer, not the driver, judges it.
  def self.set_elems(current, count)
    raise ArgumentError, "elems needs a nonnegative count, got #{count}" if count.negative?

    slots = current.class.name.to_s.end_with?('KeyedArray') ? current._s_values : current
    raise ArgumentError, "elems needs an array, got #{current.class}" unless slots.is_a?(::Array)

    first = slots[0]
    slots.slice!(count..) if slots.length > count
    slots << deep_copy(first) while slots.length < count
    current
  end

  # The 2026-11-22 kinds that replace one whole field (FORMAT.md): `elems`,
  # `mask` (a flags field's raw bits), `dupkey` (`1` holds the map's first key
  # twice, `0` leaves the map as loaded), `point` (the pointer at `path` set to
  # the node the pointer at path `token` points at) and `count -1` (SF-28: a
  # list field holding a value that is no list). Returns false for every other
  # kind, which the caller's own recipe handles.
  def self.assign_whole(value, path, rule, token)
    case rule
    when 'elems', 'mask', 'dupkey', 'point' then nil
    when 'count' then return false unless Integer(token, 10).negative?
    else return false
    end
    parts = path.tr('[', '.').delete(']').split('.').reject(&:empty?)
    owner = parts.length > 1 ? target_node(value, parts[0...-1].join('.')) : value
    name = parts[-1]
    raise ArgumentError, "#{rule} names a field, not the element #{path}" if name.match?(/\A\d+\z/)

    current = owner.public_send(name.to_sym)
    assigned =
      case rule
      when 'elems' then set_elems(current, Integer(token, 10))
      when 'mask' then Integer(token, 10)
      when 'point' then target_node(value, token)
      when 'count' then Integer(token, 10)
      else
        raise ArgumentError, "dupkey needs a map, got #{current.class}" unless current.is_a?(::Hash)

        case token
        when '0' then current
        when '1'
          # a Ruby Hash holds each key once: the map cannot hold a key twice
          raise Unrepresentable, 'a Hash cannot hold one key twice'
        else raise ArgumentError, "dupkey takes 0 or 1, got #{token}"
        end
      end
    owner.public_send(:"#{name}=", assigned)
    true
  end

  def self.module_const_name(pkg)
    pkg.split('_').map(&:capitalize).join
  end

  def self.unit_package(key, tree = nil)
    base = tree || ROOT
    cache_key = "#{base}::#{key}"
    return LOADED_UNITS[cache_key] if LOADED_UNITS.key?(cache_key)

    unitdir = File.expand_path(File.join(base, key))
    unless File.directory?(unitdir)
      LOADED_UNITS[cache_key] = nil
      return nil
    end

    Dir.children(unitdir).sort.each do |entry|
      next if entry.start_with?('_') || !entry.end_with?('.rb')

      pkg = entry.delete_suffix('.rb')
      next unless File.directory?(File.join(unitdir, pkg))

      require File.join(unitdir, entry)
      mod_name = module_const_name(pkg)
      mod = Object.const_defined?(mod_name, false) ? Object.const_get(mod_name, false) : nil
      LOADED_UNITS[cache_key] = mod
      return mod
    end

    LOADED_UNITS[cache_key] = nil
    nil
  end

  def self.root_class(unit, root, tree = nil)
    pkg = unit_package(unit, tree)
    return nil if pkg.nil?
    return nil unless pkg.const_defined?(root, false)

    pkg.const_get(root, false)
  end

  def self.fixed_carried?(pkg, cls)
    return true if cls.respond_to?(:descriptor) && cls.descriptor && cls.descriptor.variable

    pkg.respond_to?(:_s_fixed_load) ||
      (pkg.const_defined?(:SchemaTable, false) && pkg::SchemaTable.respond_to?(:fixed_load))
  end

  def self.supported?(pkg, cls)
    return false if pkg.nil? || cls.nil? || !cls.respond_to?(:descriptor) || cls.descriptor.nil?
    return false unless fixed_carried?(pkg, cls)

    seen = {}
    walk = nil
    carried = lambda do |field|
      return false if field.codec == 'unsupported'

      if %w[table pointer pointer_array].include?(field.codec) && field.target && !field.target.empty?
        return false unless pkg.const_defined?(field.target, false)

        target = pkg.const_get(field.target, false)
        return false if target.nil? || !target.respond_to?(:descriptor) || target.descriptor.nil?
        return false unless walk.call(target.descriptor)
      end
      true
    end

    walk = lambda do |desc|
      return true if seen[desc.name]

      seen[desc.name] = true
      desc.fields.each do |field|
        return false unless carried.call(field)

        elem = field.respond_to?(:elem) ? field.elem : nil
        return false if !elem.nil? && !carried.call(elem)
      end
      true
    end

    walk.call(cls.descriptor)
  end

  def self.raise_if_unsupported(err)
    reason = err.respond_to?(:reason) ? err.reason.to_s : ''
    rule = err.respond_to?(:rule) ? err.rule.to_s : ''
    if %w[unsupported unsupported_kind fixed_surface_not_selected].include?(reason) ||
       rule.include?('unsupported') || rule.include?('fixed_surface_not_selected')
      raise err
    end
  end

  def self.parse_float_token(token)
    case token.downcase
    when 'nan', '+nan', '-nan' then Float::NAN
    when 'inf', '+inf', 'infinity', '+infinity' then Float::INFINITY
    when '-inf', '-infinity' then -Float::INFINITY
    else Float(token)
    end
  end

  def self.deep_copy(obj)
    Marshal.load(Marshal.dump(obj))
  end

  def self.target_node(value, path)
    node = value
    path.tr('[', '.').delete(']').split('.').each do |part|
      next if part.empty?

      node = if part.match?(/\A\d+\z/)
               node[part.to_i]
             else
               node.public_send(part.to_sym)
             end
    end
    node
  end

  def self.field_is_bytes?(owner, name)
    return false unless owner.class.respond_to?(:descriptor) && owner.class.descriptor

    fd = owner.class.descriptor.fields.find { |f| f.name == name }
    return false if fd.nil?

    inner = fd.codec == 'optional' && fd.elem ? fd.elem : fd
    %w[bytes bytes_blob].include?(inner.codec)
  end

  def self.convert_leaf(owner, name, current, rule, token)
    case rule
    when 'count'
      count = Integer(token, 10)
      if count.negative? || !current.is_a?(::Array) || (count.positive? && current.empty?)
        raise ArgumentError, 'count mutation needs a nonnegative count and a populated list'
      end
      resized = current[0, count] || []
      resized << deep_copy(current[0]) while resized.length < count
      resized
    when 'length'
      length = Integer(token, 10)
      raise ArgumentError, 'length cannot be negative' if length.negative?

      is_bytes = current.nil? ? field_is_bytes?(owner, name) : (current.encoding == ::Encoding::BINARY)
      is_bytes ? ('a'.b * length) : ('a' * length)
    when 'ordinal', 'int'
      Integer(token, 10)
    when 'tag'
      union_val = current.class.new
      union_val.type = Integer(token, 10)
      union_val
    when 'bool'
      %w[true 1].include?(token.downcase)
    when 'float'
      parse_float_token(token)
    when 'bytes'
      raw = [token].pack('H*')
      is_bytes = current.nil? ? field_is_bytes?(owner, name) : (current.encoding == ::Encoding::BINARY)
      is_bytes ? raw : raw.dup.force_encoding(::Encoding::UTF_8)
    else
      raise ArgumentError, "unknown write-refusal mutation: #{rule}"
    end
  end

  def self.assign_mutation(value, path, rule, token)
    return if assign_whole(value, path, rule, token)

    parts = path.tr('[', '.').delete(']').split('.').reject(&:empty?)
    if parts.length >= 3 && parts[-2].match?(/\A\d+\z/)
      parent = parts.length > 3 ? target_node(value, parts[0...-3].join('.')) : value
      container = parent.public_send(parts[-3].to_sym)
      if container.is_a?(::Hash)
        idx = parts[-2].to_i
        old_k = container.keys.sort[idx]
        old_v = container[old_k]
        if parts[-1] == 'key'
          new_k = convert_leaf(nil, 'key', old_k, rule, token)
          container.delete(old_k)
          container[new_k] = old_v
        else
          container[old_k] = convert_leaf(nil, 'value', old_v, rule, token)
        end
        return
      end
    end
    owner = parts.length > 1 ? target_node(value, parts[0...-1].join('.')) : value
    name = parts[-1]
    if name.match?(/\A\d+\z/)
      idx = name.to_i
      idx += 1 if owner.class.name.end_with?('KeyedArray')
      current = owner[idx]
      owner[idx] = convert_leaf(owner, name, current, rule, token)
    else
      current = owner.public_send(name.to_sym)
      owner.public_send(:"#{name}=", convert_leaf(owner, name, current, rule, token))
    end
  end
end
