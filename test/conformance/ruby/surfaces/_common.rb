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

  # The cases a surface answers, ONE AT A TIME. A case has three outcomes and
  # the driver tells them apart by mechanism, never by name: no codec for it
  # (an EMPTY `.absent`, below), the present codec's answer (bytes, or its
  # refusal), or a DRIVER ERROR. A driver error names its case on stderr and
  # the cases behind it still run; the surface then exits non-zero, so the
  # harness reds it (test/conformance/README.md: exit codes).
  def self.cases(manifest, kind)
    entries(manifest, kind).each do |row|
      yield row
    rescue StandardError => e
      @driver_errors = (@driver_errors || 0) + 1
      $stderr.write("#{@surface} #{row[1]}: driver error: #{e.class}: #{e.message}\n")
    end
  end

  # the surface main.rb is running, for the driver-error line
  def self.surface=(name)
    @surface = name
  end

  def self.driver_errors
    @driver_errors || 0
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

  # A `.absent` that names a reason carries that reason as the leg's answer,
  # and on a row that owes a refusal the harness scores it as the refusal. A
  # refusal is never spelled empty: that is the no-codec absence below.
  def self.absent(outdir, name, reason)
    text = reason.to_s.strip
    write_bytes(outdir, "#{name}.absent", (text.empty? ? 'refused' : text).b)
  end

  # A case this leg has NO CODEC for (its unit or root is not generated, or the
  # form is not emitted) is an EMPTY `.absent`: the per-case absence of
  # test/conformance/README.md, which counts absent and never as a refusal, so
  # a missing codec cannot pass a must-refuse row.
  def self.no_codec(outdir, name)
    write_bytes(outdir, "#{name}.absent", ''.b)
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

  # The default value of one element of the list field `name` on owner, by the
  # field's declared element kind (its descriptor), for a count mutation that
  # grows an empty list.
  def self.element_default(owner, name)
    desc = owner.class.respond_to?(:descriptor) ? owner.class.descriptor : nil
    fd = desc&.fields&.find { |f| f.name == name }
    raise ArgumentError, "count mutation: #{name} has no field descriptor" if fd.nil?

    elem = fd.respond_to?(:elem) ? fd.elem : nil
    unless elem.nil?
      value = elem.default_value
      return value unless value.nil?
    end
    target = fd.target.to_s
    unless target.empty?
      pkg = Object.const_get(owner.class.name.split('::').first)
      return pkg.const_get(target, false).new if pkg.const_defined?(target, false)
    end
    case fd.elem_codec
    when 'bool' then false
    when 'float32', 'float64' then 0.0
    when 'string' then +''
    when 'bytes' then ''.b
    when 'int', 'enum' then 0
    else raise ArgumentError, "count mutation: no element default for #{name} (#{fd.elem_codec})"
    end
  end

  def self.convert_leaf(owner, name, current, rule, token)
    case rule
    when 'count'
      count = Integer(token, 10)
      if count.negative? || !current.is_a?(::Array)
        raise ArgumentError, 'count mutation needs a nonnegative count and a list'
      end
      resized = current[0, count] || []
      # an empty list grows by its DECLARED element kind's default, never by
      # copying an element it does not have
      grown = current.empty? && count.positive? ? element_default(owner, name) : nil
      resized << deep_copy(current.empty? ? grown : current[0]) while resized.length < count
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
