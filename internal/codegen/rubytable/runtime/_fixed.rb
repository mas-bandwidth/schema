# frozen_string_literal: true

# Form 3: baked plans over fixed record images.
#
# The generated _fixed_data file supplies every layout, record size, schema and
# operation. A file selects a known hash and compares bytes; it never supplies
# an executable layout. All records are checked before any value is built.

module SchemaRuntimeUnit
  class LoadReport
    def reset
      @unknown = 0
      @widened = 0
      @retained = 0
      @retain_lost = 0
    end
  end

  class FixedLoadReport < LoadReport
    attr_accessor :layout_hash

    def initialize
      super
      @layout_hash = 0
    end

    def reset
      super
      @layout_hash = 0
    end
  end

  @_s_fixed_roots = {} #: ::Hash[String, untyped]
  @_s_fixed_classes = {} #: ::Hash[String, untyped]
  @_s_crc32c_table = ::Array.new(256) do |n|
    crc = n
    8.times do
      crc = (crc >> 1) ^ ((crc & 1) != 0 ? 0x82F63B78 : 0)
    end
    crc
  end.freeze #: ::Array[Integer]

  def self._s_fixed_roots
    @_s_fixed_roots
  end

  def self._s_fixed_classes
    @_s_fixed_classes
  end

  def self._s_fixed_arr
    [] #: ::Array[untyped]
  end

  def self._s_fixed_hash
    {} #: ::Hash[String, untyped]
  end

  def self._s_fixed_crc32c(data)
    value = 0xFFFFFFFF
    tbl = @_s_crc32c_table
    data.each_byte do |byte|
      value = (tbl[(value ^ byte) & 0xFF] || 0) ^ (value >> 8)
    end
    value ^ 0xFFFFFFFF
  end

  def self._s_fixed_seal_crc(data)
    data + [_s_fixed_crc32c(data)].pack("L<")
  end

  def self._s_fixed_form_refusal(form, b0)
    if form == 1
      return "message_form_as_file" if b0 == 2
      return "fixed_form_as_file" if b0 == 3
      return "newer_form"
    end
    if form == 3
      return "previous_form" if b0 == 1
      return "message_form_as_file" if b0 == 2
      return "newer_form"
    end
    "newer_form"
  end

  def self._s_fixed_open_crc(data, form = 3)
    if data.bytesize == 0
      raise Refused.new(form == 3 ? "truncated" : "wire_damaged", "empty file")
    end
    b0 = data.getbyte(0) || 0
    if b0 != form
      raise Refused.new(_s_fixed_form_refusal(form, b0), "wrong form")
    end
    if data.bytesize < (form == 2 ? 6 : 5)
      raise Refused.new("integrity_mismatch", "file is shorter than its checksum")
    end
    body = data.byteslice(0, data.bytesize - 4) || "".b
    trailer = (data.byteslice(data.bytesize - 4, 4) || "".b).unpack1("L<") #: Integer
    if trailer != _s_fixed_crc32c(body)
      raise Refused.new("integrity_mismatch", "CRC-32C does not match")
    end
    body
  end

  def self._s_fixed_int(data, at, width, signed)
    if width == 1
      return data.getbyte(at) || 0 unless signed
      v1 = (data.byteslice(at, 1) || "".b).unpack1("c") #: Integer?
      return v1 || 0
    elsif width == 2
      v2 = (data.byteslice(at, 2) || "".b).unpack1(signed ? "s<" : "S<") #: Integer?
      return v2 || 0
    elsif width == 4
      v4 = (data.byteslice(at, 4) || "".b).unpack1(signed ? "l<" : "L<") #: Integer?
      return v4 || 0
    elsif width == 8
      v8 = (data.byteslice(at, 8) || "".b).unpack1(signed ? "q<" : "Q<") #: Integer?
      return v8 || 0
    elsif width == 16
      pair = (data.byteslice(at, 16) || "".b).unpack(signed ? "Q<q<" : "Q<Q<")
      lo_raw = pair[0] #: Integer?
      hi_raw = pair[1] #: Integer?
      lo = lo_raw || 0
      hi = hi_raw || 0
      return (hi << 64) | lo
    end
    acc = 0
    i = 0
    while i < width
      acc |= (data.getbyte(at + i) || 0) << (i * 8)
      i += 1
    end
    if signed && width > 0 && (acc & (1 << ((width * 8) - 1))) != 0
      acc -= (1 << (width * 8))
    end
    acc
  end

  def self._s_fixed_store(out, at, width, value)
    if width == 1
      out.setbyte(at, value & 0xFF)
    elsif width == 2
      out[at, 2] = [value & 0xFFFF].pack("S<")
    elsif width == 4
      out[at, 4] = [value & 0xFFFFFFFF].pack("L<")
    elsif width == 8
      out[at, 8] = [value & 0xFFFFFFFFFFFFFFFF].pack("Q<")
    elsif width == 16
      out[at, 16] = [value & 0xFFFFFFFFFFFFFFFF, (value >> 64) & 0xFFFFFFFFFFFFFFFF].pack("Q<Q<")
    else
      i = 0
      while i < width
        out.setbyte(at + i, (value >> (i * 8)) & 0xFF)
        i += 1
      end
    end
  end

  def self._s_fixed_dead(data, from, stop)
    i = from
    while i < stop
      if (data.getbyte(i) || 0) != 0
        raise Refused.new("wire_damaged", "nonzero bytes outside the live extent")
      end
      i += 1
    end
  end

  def self._s_fixed_bool(data, at)
    value = data.getbyte(at) || 0
    if value > 1
      raise Refused.new("wire_damaged", "a bool or presence byte is not 0 or 1")
    end
    value == 1
  end

  def self._s_fixed_text(raw)
    str = ::String.new(capacity: raw.bytesize, encoding: ::Encoding::BINARY)
    str << raw
    str.force_encoding(::Encoding::UTF_8)
    raise Refused.new("text_ill_formed") unless str.valid_encoding?
    if str.include?("\x00")
      raise Refused.new("text_ill_formed")
    end
    str
  end

  def self._s_fixed_encode(spec, value, out, at, path, check)
    shape = spec["shape"]
    size = spec["size"]
    if shape == "optional"
      unless value.nil?
        out.setbyte(at, 1)
        _s_fixed_encode(spec["children"][0], value, out, at + 1, path, check)
      end
      return
    end
    if shape == "table"
      target_name = spec["target"]
      cls = @_s_fixed_classes[target_name]
      if cls.nil? || !value.is_a?(cls)
        raise InvalidValue.new(path, "not a #{target_name}")
      end
      cursor = at
      spec["children"].each do |field|
        fname = field["name"]
        _s_fixed_encode(field, value.public_send(fname.to_sym), out, cursor, "#{path}.#{fname}", check)
        cursor += field["size"]
      end
      return
    end
    if shape == "array" || shape == "keyed"
      if shape == "keyed" && !spec["positional"]
        unless value.respond_to?(:_s_values)
          raise InvalidValue.new(path, "not a keyed array")
        end
        values = value._s_values
      else
        values = value
      end
      unless values.instance_of?(::Array)
        raise InvalidValue.new(path, "not an Array")
      end
      count = values.length
      if spec["counted"]
        min_bound = spec["minimum"] || 0
        if count < min_bound || count > spec["bound"]
          raise InvalidValue.new(path, "count outside its declared bound")
        end
        _s_fixed_store(out, at, 4, count)
        at += 4
      elsif count != spec["bound"]
        raise InvalidValue.new(path, "wrong fixed array length")
      end
      elem = spec["children"][0]
      esize = elem["size"] #: Integer
      values.each_with_index do |item, index|
        _s_fixed_encode(elem, item, out, at + (index * esize), "#{path}[#{index}]", check)
      end
      return
    end
    if shape == "union"
      target_name = spec["target"]
      cls = @_s_fixed_classes[target_name]
      if cls.nil? || !value.is_a?(cls)
        raise InvalidValue.new(path, "not a #{target_name}")
      end
      tag = value.type
      arms = spec["children"]
      unless tag.is_a?(::Integer) && tag >= 0 && tag <= arms.length
        raise InvalidValue.new(path, "union tag has no arm")
      end
      _s_fixed_store(out, at, spec["tag"], tag)
      if tag > 0
        arm = arms[tag - 1]
        if arm["shape"] != "void"
          aname = arm["name"]
          _s_fixed_encode(arm, value.public_send(aname.to_sym), out, at + spec["tag"], "#{path}.#{aname}", check)
        end
      end
      return
    end
    if shape == "bool"
      unless value.equal?(true) || value.equal?(false)
        raise InvalidValue.new(path, "not a bool")
      end
      out.setbyte(at, value ? 1 : 0)
    elsif shape == "int" || shape == "enum"
      unless value.is_a?(::Integer)
        raise InvalidValue.new(path, "not an Integer")
      end
      lo = spec["lo"] || 0
      hi = spec["hi"] || (spec["variants"] ? spec["variants"].length : 0)
      deprecated = spec["deprecated"]
      if value < lo || value > hi || (check && deprecated&.include?(value))
        raise InvalidValue.new(path, "integer outside its declared range or set")
      end
      _s_fixed_store(out, at, size, value)
    elsif shape == "float"
      unless value.is_a?(::Float) && value.finite?
        raise InvalidValue.new(path, "not a finite Float")
      end
      if spec["ranged"] && value == 0.0
        value = 0.0
      end
      if size == 4
        stored = Serialize.float32_round(value)
        unless stored.finite?
          raise InvalidValue.new(path, "float overflows its storage")
        end
        if spec["ranged"] && (stored < spec["lo"] || stored > spec["hi"])
          raise InvalidValue.new(path, "float outside its declared range")
        end
        out[at, 4] = [stored].pack("e")
      else
        if spec["ranged"] && (value < spec["lo"] || value > spec["hi"])
          raise InvalidValue.new(path, "float outside its declared range")
        end
        out[at, 8] = [value].pack("E")
      end
    elsif shape == "string" || shape == "bytes"
      if shape == "string"
        unless value.is_a?(::String)
          raise InvalidValue.new(path, "not NUL-free text")
        end
        enc = value.encoding
        unless enc == ::Encoding::UTF_8 || enc == ::Encoding::US_ASCII
          raise InvalidValue.new(path, "text is not UTF-8")
        end
        unless value.valid_encoding?
          raise InvalidValue.new(path, "text is not UTF-8")
        end
        if value.include?("\x00")
          raise InvalidValue.new(path, "not NUL-free text")
        end
        raw = value.b
      else
        unless value.is_a?(::String) && value.encoding == ::Encoding::BINARY
          raise InvalidValue.new(path, "not ASCII-8BIT bytes")
        end
        raw = value
      end
      if raw.bytesize > spec["bound"]
        raise InvalidValue.new(path, "length outside its declared bound")
      end
      _s_fixed_store(out, at, 4, raw.bytesize)
      out[at + 4, raw.bytesize] = raw if raw.bytesize > 0
    elsif shape != "void"
      raise InvalidValue.new(path, "unsupported fixed value")
    end
  end

  def self._s_fixed_seed(spec, out, at)
    shape = spec["shape"]
    if shape == "table"
      spec["children"].each do |field|
        _s_fixed_seed(field, out, at)
        at += field["size"]
      end
    elsif shape == "array" || shape == "keyed"
      count = spec["counted"] ? (spec["minimum"] || 0) : spec["bound"]
      if spec["counted"]
        _s_fixed_store(out, at, 4, count)
        at += 4
      end
      elem = spec["children"][0]
      esize = elem["size"] #: Integer
      count.times do |index|
        _s_fixed_seed(elem, out, at + (index * esize))
      end
    elsif shape != "optional" && shape != "union" && shape != "void"
      def_val = spec.key?("default") ? spec["default"] : 0
      _s_fixed_encode(spec, def_val, out, at, "default", false)
    end
  end

  def self._s_fixed_check(spec, data, at)
    shape = spec["shape"]
    size = spec["size"]
    if shape == "optional"
      if _s_fixed_bool(data, at)
        _s_fixed_check(spec["children"][0], data, at + 1)
      else
        _s_fixed_dead(data, at + 1, at + size)
      end
    elsif shape == "table"
      spec["children"].each do |field|
        _s_fixed_check(field, data, at)
        at += field["size"]
      end
    elsif shape == "array" || shape == "keyed"
      elem = spec["children"][0]
      esize = elem["size"] #: Integer
      count = spec["bound"] #: Integer
      if spec["counted"]
        count = _s_fixed_int(data, at, 4, true)
        min_bound = spec["minimum"] || 0
        if count < min_bound || count > spec["bound"]
          raise Refused.new("count_over_bound")
        end
        _s_fixed_dead(data, at + 4 + (count * esize), at + size)
        at += 4
      end
      count.times do |index|
        _s_fixed_check(elem, data, at + (index * esize))
      end
    elsif shape == "union"
      tag = _s_fixed_int(data, at, spec["tag"], false)
      arms = spec["children"]
      if tag > arms.length
        raise Refused.new("out_of_range")
      end
      used = spec["tag"]
      if tag > 0
        arm = arms[tag - 1]
        used += arm["size"]
        _s_fixed_check(arm, data, at + spec["tag"])
      end
      _s_fixed_dead(data, at + used, at + size)
    elsif shape == "bool"
      _s_fixed_bool(data, at)
    elsif shape == "int" || shape == "enum"
      value = _s_fixed_int(data, at, size, spec["signed"] || false)
      lo = spec["lo"] || 0
      hi = spec["hi"] || (spec["variants"] ? spec["variants"].length : 0)
      if value < lo || value > hi
        raise Refused.new("out_of_range")
      end
      if shape == "enum" && value > spec["variants"].length
        raise Refused.new("out_of_range")
      end
    elsif shape == "float"
      raw = _s_fixed_int(data, at, size, false)
      exponent = size == 4 ? 0x7F800000 : 0x7FF0000000000000
      if (raw & exponent) == exponent
        raise Refused.new("non_finite_float")
      end
      fval = (data.byteslice(at, size) || "".b).unpack1(size == 4 ? "e" : "E") #: Float
      if spec["ranged"] && (fval < spec["lo"] || fval > spec["hi"])
        raise Refused.new("out_of_range")
      end
    elsif shape == "string" || shape == "bytes"
      count = _s_fixed_int(data, at, 4, true)
      if count < 0 || count > spec["bound"]
        raise Refused.new("length_over_bound")
      end
      _s_fixed_dead(data, at + 4 + count, at + size)
      if shape == "string"
        _s_fixed_text(data.byteslice(at + 4, count) || "".b)
      end
    end
  end

  def self._s_fixed_decode(spec, data, at, current = nil)
    shape = spec["shape"]
    size = spec["size"]
    if shape == "optional"
      return (data.getbyte(at) || 0) != 0 ? _s_fixed_decode(spec["children"][0], data, at + 1) : nil
    end
    if shape == "table"
      value = @_s_fixed_classes[spec["target"]].new
      spec["children"].each do |field|
        fname = field["name"]
        prev = value.public_send(fname.to_sym)
        value.public_send(:"#{fname}=", _s_fixed_decode(field, data, at, prev))
        at += field["size"]
      end
      return value
    end
    if shape == "array" || shape == "keyed"
      n = spec["bound"]
      if spec["counted"]
        n = _s_fixed_int(data, at, 4, false)
        at += 4
      end
      elem = spec["children"][0]
      esize = elem["size"] #: Integer
      values = ::Array.new(n) { |index| _s_fixed_decode(elem, data, at + (index * esize)) }
      if shape == "keyed" && !spec["positional"]
        holder = current || @_s_fixed_classes[spec["keyed_class"]].new
        holder._s_values.replace(values)
        return holder
      end
      return values
    end
    if shape == "union"
      value = @_s_fixed_classes[spec["target"]].new
      value.type = _s_fixed_int(data, at, spec["tag"], false)
      if value.type > 0
        arm = spec["children"][value.type - 1]
        if arm["shape"] != "void"
          aname = arm["name"]
          prev = value.public_send(aname.to_sym)
          value.public_send(:"#{aname}=", _s_fixed_decode(arm, data, at + spec["tag"], prev))
        end
      end
      return value
    end
    if shape == "bool"
      return (data.getbyte(at) || 0) == 1
    end
    if shape == "int" || shape == "enum"
      value = _s_fixed_int(data, at, size, spec["signed"] || false)
      if shape == "enum" && spec["deprecated"]&.include?(value)
        return spec["default"] || 0
      end
      return value
    end
    if shape == "float"
      result = (data.byteslice(at, size) || "".b).unpack1(size == 4 ? "e" : "E") #: Float
      return spec["ranged"] && result == 0.0 ? 0.0 : result
    end
    if shape == "string" || shape == "bytes"
      n = _s_fixed_int(data, at, 4, false)
      slice = data.byteslice(at + 4, n) || "".b
      if shape == "string"
        return _s_fixed_text(slice)
      end
      out = ::String.new(capacity: n, encoding: ::Encoding::BINARY)
      out << slice
      return out
    end
    nil
  end

  def self._s_fixed_guarded(op, source)
    op["guards"].each do |g|
      actual = _s_fixed_int(source, g[0], g[1], false)
      mode = g[3]
      return false if (mode == "eq" && actual != g[2]) || (mode == "gt" && actual <= g[2])
    end
    true
  end

  def self._s_fixed_apply(op, source, image)
    src = op["src"]
    dst = op["dst"]
    size = op["size"]
    kind = op["op"]
    if kind == "copy"
      image[dst, size] = source.byteslice(src, size) || "".b if size > 0
    elsif kind == "widen"
      _s_fixed_store(image, dst, op["width"], _s_fixed_int(source, src, size, op["signed"]))
      return 1
    elsif kind == "widenf"
      f32 = (source.byteslice(src, 4) || "".b).unpack1("e") #: Float
      image[dst, 8] = [f32].pack("E")
      return 1
    elsif kind == "const"
      _s_fixed_store(image, dst, size, op["value"])
    elsif kind == "count"
      image[dst, 4] = source.byteslice(src, 4) || "".b
    elsif kind == "text"
      image[dst, 4 + size] = source.byteslice(src, 4 + size) || "".b
    elsif kind == "ordinal"
      ordinal = _s_fixed_int(source, src, size, false)
      _s_fixed_store(image, dst, op["width"], op["remap"][ordinal])
      return size < op["width"] ? 1 : 0
    end
    0
  end

  def self._s_fixed_register(roots)
    roots.each do |name, root|
      layout_bytes = [root["layout"]].pack("H*")
      root["layout"] = layout_bytes
      prefix = [root["hash"]].pack("Q<")
      root["prefix"] = prefix
      root["header"] = ("\x03\x00\x00\x00\x00\x00\x00\x00".b + prefix + [layout_bytes.bytesize].pack("L<") + layout_bytes).freeze
      reset = "\x00".b * root["body"]
      _s_fixed_seed(root["spec"], reset, 0)
      root["reset"] = reset.freeze
      known = {} #: ::Hash[Integer, untyped]
      root["known"].each do |entry|
        entry["layout"] = [entry["layout"]].pack("H*").freeze
        if entry["refusal"].empty?
          entry["check"] = entry["check_spec"]
          entry["fills"].each do |fill|
            img = "\x00".b * fill["size"]
            _s_fixed_seed(fill["spec"], img, 0)
            fill["image"] = img.freeze
          end
          cover = ::Array.new(root["body"], false)
          entry["plan"].each do |op|
            cnt = op["size"]
            if op["op"] == "widen" || op["op"] == "widenf" || op["op"] == "ordinal"
              cnt = op["width"]
            elsif op["op"] == "count"
              cnt = 4
            elsif op["op"] == "text"
              cnt += 4
            end
            j = 0
            while j < cnt
              cover[op["dst"] + j] = true
              j += 1
            end
          end
          holes = [] #: ::Array[[Integer, Integer]]
          at = 0
          while at < cover.length
            if cover[at]
              at += 1
              next
            end
            stop = at + 1
            stop += 1 while stop < cover.length && !cover[stop]
            holes << [at, stop]
            at = stop
          end
          entry["holes"] = holes
          split_idx = entry["plan"].index { |op| !op["guards"].empty? }
          entry["split"] = split_idx.nil? ? entry["plan"].length : split_idx
          entry["direct"] = (
            entry["record"] == root["body"] + 8 &&
            holes.empty? &&
            entry["fills"].empty? &&
            entry["plan"].all? { |op| op["op"] == "copy" && op["guards"].empty? && op["src"] == op["dst"] }
          )
        end
        known[entry["hash"]] = entry
      end
      root["known"] = known
      cls = @_s_fixed_classes[name]
      cls.fixed_plan_max = root["plan_max"] unless cls.nil?
      @_s_fixed_roots[name] = root
    end
  end

  def self._s_fixed_root(cls_name, writing)
    root = @_s_fixed_roots[cls_name]
    if root.nil?
      raise InvalidValue.new(cls_name, "layout_unsupported") if writing
      raise Refused.new("layout_unsupported")
    end
    root
  end

  def self._s_fixed_measure(cls_name, count)
    root = _s_fixed_root(cls_name, true)
    unless count.is_a?(::Integer) && count >= 1
      raise InvalidValue.new(cls_name, "a fixed file holds at least one record")
    end
    root["header"].bytesize + (count * (root["body"] + 8)) + (root["integrity"] ? 4 : 0)
  end

  def self._s_fixed_save(cls_name, cls_obj, values)
    root = _s_fixed_root(cls_name, true)
    unless values.instance_of?(::Array) && !values.empty?
      raise InvalidValue.new(cls_name, "a fixed file holds at least one record")
    end
    records = ::Array.new(values.length) do |index|
      value = values[index]
      unless value.is_a?(cls_obj)
        raise InvalidValue.new("[#{index}]", "not a #{cls_name}")
      end
      image = "\x00".b * root["body"]
      _s_fixed_encode(root["spec"], value, image, 0, "[#{index}]", true)
      root["prefix"] + image
    end
    result = root["header"] + records.join
    root["integrity"] ? _s_fixed_seal_crc(result) : result
  end

  def self._s_fixed_load(cls_name, data, capacity = 4096, report = nil, plan_capacity = nil)
    raise Refused.new("bad_input", "expected String") unless data.is_a?(::String)
    source = data.encoding == ::Encoding::BINARY ? data : data.b
    raise Refused.new("truncated") if source.bytesize == 0
    b0 = source.getbyte(0) || 0
    if b0 != 3
      raise Refused.new(_s_fixed_form_refusal(3, b0))
    end
    declared_root = @_s_fixed_roots[cls_name]
    if !declared_root.nil? && declared_root["integrity"]
      source = _s_fixed_open_crc(source, 3)
    end
    raise Refused.new("truncated") if source.bytesize < 20
    _s_fixed_dead_header(source)
    length = _s_fixed_int(source, 16, 4, false)
    if length < 4 || 20 + length > source.bytesize
      raise Refused.new("layout_malformed")
    end
    root = _s_fixed_root(cls_name, false)
    wire_hash = _s_fixed_int(source, 8, 8, false)
    entry = root["known"][wire_hash]
    if entry.nil?
      raise Refused.new("layout_unknown", format("0x%016x", wire_hash))
    end
    if entry["refusal"] == "layout_unsupported"
      raise Refused.new("layout_unsupported", format("0x%016x", wire_hash))
    end
    if source.byteslice(20, length) != entry["layout"]
      raise Refused.new("layout_malformed")
    end
    unless entry["refusal"].empty?
      raise Refused.new(entry["refusal"])
    end
    if !plan_capacity.nil? && (!plan_capacity.is_a?(::Integer) || plan_capacity < entry["plan"].length + entry["fills"].length)
      raise Refused.new("plan_too_large")
    end
    record = entry["record"] #: Integer
    rest = source.bytesize - 20 - length
    if record <= 8 || rest <= 0 || (rest % record) != 0
      raise Refused.new("truncated")
    end
    count = rest / record
    if !capacity.is_a?(::Integer) || capacity < 0 || count > capacity
      raise Refused.new("batch_too_large")
    end
    start = 20 + length
    count.times do |index|
      at = start + (index * record)
      if _s_fixed_int(source, at, 8, false) != wire_hash
        raise Refused.new("no_layout")
      end
      _s_fixed_check(entry["check"], source, at + 8)
    end
    widened = 0
    direct = entry["direct"]
    spec = root["spec"]
    if direct
      values = ::Array.new(count) { |index| _s_fixed_decode(spec, source, start + (index * record) + 8) }
    else
      values = [] #: ::Array[untyped]
      count.times do |index|
        at = start + (index * record) + 8
        body = source.byteslice(at, record - 8) || "".b
        image = "\x00".b * root["body"]
        entry["holes"].each do |hole|
          h_begin = hole[0]
          h_end = hole[1]
          image[h_begin, h_end - h_begin] = root["reset"].byteslice(h_begin, h_end - h_begin) || "".b
        end
        entry["fills"].each do |fill|
          if fill["guards"].empty? || _s_fixed_guarded(fill, body)
            image[fill["dst"], fill["size"]] = fill["image"]
          end
        end
        plan = entry["plan"]
        split = entry["split"]
        i = 0
        while i < split
          widened += _s_fixed_apply(plan[i], body, image)
          i += 1
        end
        while i < plan.length
          op = plan[i]
          widened += _s_fixed_apply(op, body, image) if _s_fixed_guarded(op, body)
          i += 1
        end
        values << _s_fixed_decode(spec, image, 0)
      end
    end
    unless report.nil?
      report.reset
      report.widened = widened
      if report.is_a?(FixedLoadReport)
        report.layout_hash = wire_hash
      end
    end
    values
  end

  def self._s_fixed_dead_header(source)
    i = 1
    while i < 8
      raise Refused.new("reserved_not_zero") if (source.getbyte(i) || 0) != 0
      i += 1
    end
  end
end
