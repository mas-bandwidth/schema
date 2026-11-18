# frozen_string_literal: true

module SchemaRuntimeUnit
  # Strict JSON reader and canonical JSON writer over generated descriptors.
  module SchemaTable
    WIDE_INTEGER_KINDS = { 5 => true, 9 => true, 18 => true, 19 => true }.freeze
    FIXED_POINT_KINDS = (20..29).to_a.to_h { |k| [k, true] }.freeze
    WIDE_FIXED_KINDS = { 23 => true, 24 => true, 28 => true, 29 => true }.freeze
    DECIMAL_RE = /\A(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?\z/
    NEG_ZERO_F = [0x80000000].pack('V').unpack1('e')
    TIMESTAMP_RE = Regexp.new(
      '\\A([0-9]{4})-([0-9]{2})-([0-9]{2})[Tt]([0-9]{2}):([0-9]{2}):([0-9]{2})' \
      '(?:\\.([0-9]+))?([Zz]|[+-][0-9]{2}:[0-9]{2})\\z'
    )

    JSON_QUOTE_MAP = {
      '"' => '\\"',
      '\\' => '\\\\',
      "\b" => '\\b',
      "\f" => '\\f',
      "\n" => '\\n',
      "\r" => '\\r',
      "\t" => '\\t'
    }.freeze

    def self.json_quote(str)
      out = String.new('"', encoding: Encoding::UTF_8)
      str.each_char do |ch|
        esc = JSON_QUOTE_MAP[ch]
        out << if esc
                 esc
               elsif ch.ord < 32
                 format('\\u%04x', ch.ord)
               else
                 ch
               end
      end
      out << '"'
      out
    end

    def self.signed_zero(neg)
      neg ? NEG_ZERO_F : 0.0
    end

    def self.parse_json_float32(token, field)
      m = DECIMAL_RE.match(token)
      raise Refused.new('kind_mismatch', field.name) if m.nil?

      frac = m[3] || ''
      exponent = m[4] || '0'
      digits = m[2] + frac
      neg = !m[1].empty?
      return signed_zero(neg) if digits.delete('0').empty?

      exp_digits = exponent.sub(/\A[+-]?0*/, '')
      if exp_digits.length > 6
        return signed_zero(neg) if exponent.start_with?('-')

        raise Refused.new('out_of_range', field.name)
      end
      exp = (exp_digits.empty? ? 0 : exp_digits.to_i) * (exponent.start_with?('-') ? -1 : 1)
      point = m[2].length + exp
      trimmed = digits.sub(/\A0+/, '')
      point -= (digits.length - trimmed.length)
      digits = trimmed.sub(/0+\z/, '')
      raise Refused.new('out_of_range', field.name) if point > 40
      return signed_zero(neg) if point < -60

      if digits.length > 200
        tail = digits[200..]
        digits = digits[0, 200] + (tail.delete('0').empty? ? '' : '1')
      end
      num = digits.to_i
      scale = point - digits.length
      den = 1
      if scale >= 0
        num *= (10**scale)
      else
        den = u(10**(-scale))
      end
      exp2 = num.bit_length - den.bit_length
      below = exp2 >= 0 ? num < (den << exp2) : (num << (-exp2)) < den
      exp2 -= 1 if below
      shift = exp2 < -126 ? 149 : 23 - exp2
      if shift >= 0
        num <<= shift
      else
        den <<= (-shift)
      end
      mantissa, rem = num.divmod(den)
      mantissa += 1 if (rem * 2 > den) || ((rem * 2 == den) && mantissa.anybits?(1))
      if exp2 >= -126
        if mantissa >= (1 << 24)
          exp2 += 1
          mantissa >>= 1
        end
        raise Refused.new('out_of_range', field.name) if exp2 > 127

        bits = ((exp2 + 127) << 23) | (mantissa & ((1 << 23) - 1))
      else
        bits = mantissa
      end
      bits |= (1 << 31) if neg
      [bits].pack('V').unpack1('e')
    end

    def self.read_json_float(field, token)
      raise Refused.new('kind_mismatch', field.name) unless token.is_a?(JsonNumber)

      val = 0.0
      old_v = $VERBOSE
      begin
        $VERBOSE = nil
        val = field.kind == 10 ? parse_json_float32(token.token, field) : Float(token.token)
      ensure
        $VERBOSE = old_v
      end
      raise Refused.new('out_of_range', field.name) unless val.finite?

      if field.float_range
        raise Refused.new('out_of_range', field.name) unless val.between?(field.fmin, field.fmax)

        val = 0.0 if val.zero?
      end
      val
    rescue ArgumentError
      raise Refused.new('kind_mismatch', field.name)
    end

    def self.write_json_float(field, value)
      raise InvalidValue.new(field.name, 'not a finite float') unless value.instance_of?(Float) && value.finite?

      single = field.kind == 10
      if single
        rounded = u([value].pack('e').unpack1('e'))
        raise InvalidValue.new(field.name, 'float32 overflow') unless rounded.finite?

        value = rounded
      end
      if field.float_range
        raise InvalidValue.new(field.name, 'float is outside its declared range') unless value.between?(field.fmin,
                                                                                                        field.fmax)

        value = 0.0 if value.zero?
      end
      low, high = single ? [6, 9] : [15, 17]
      (low..high).each do |prec|
        txt = format("%.#{prec}g", value)
        parsed = 0.0
        old_v = $VERBOSE
        begin
          $VERBOSE = nil
          parsed = single ? parse_json_float32(txt, field) : Float(txt)
        ensure
          $VERBOSE = old_v
        end
        return txt if prec == high || parsed == value
      end
      format("%.#{high}g", value)
    end

    def self.read_json_timestamp(field, value)
      raise Refused.new('kind_mismatch', field.name) unless value.instance_of?(String)

      m = TIMESTAMP_RE.match(value)
      raise Refused.new('timestamp_not_rfc3339', field.name) if m.nil?

      yr = m[1].to_i
      mo = m[2].to_i
      dy = m[3].to_i
      hr = m[4].to_i
      mi = m[5].to_i
      sc = m[6].to_i
      unless yr.between?(1, 9999) && mo.between?(1, 12) && dy.between?(1, 31) &&
             hr.between?(0, 23) && mi.between?(0, 59) && sc.between?(0, 59)
        raise Refused.new('timestamp_not_rfc3339', field.name)
      end

      begin
        stamp = Time.utc(yr, mo, dy, hr, mi, sc)
      rescue ArgumentError
        raise Refused.new('timestamp_not_rfc3339', field.name)
      end
      raise Refused.new('timestamp_not_rfc3339', field.name) if stamp.year != yr || stamp.month != mo || stamp.day != dy

      offset = m[8]
      shift = 0
      unless offset.casecmp('Z').zero?
        oh = offset[1, 2].to_i
        om = offset[4, 2].to_i
        raise Refused.new('timestamp_not_rfc3339', field.name) if oh > 23 || om > 59

        shift = ((oh * 3600) + (om * 60)) * (offset.start_with?('-') ? -1 : 1)
      end
      frac = (m[7] || '')[0, 9].ljust(9, '0')
      nanos = ((stamp.to_i - shift) * 1_000_000_000) + frac.to_i
      min_i64 = -(1 << 63)
      max_i64 = (1 << 63) - 1
      raise Refused.new('timestamp_not_rfc3339', field.name) unless nanos.between?(min_i64, max_i64)

      nanos
    end

    def self.write_json_timestamp(field, value)
      min_i64 = -(1 << 63)
      max_i64 = (1 << 63) - 1
      unless value.instance_of?(Integer) && value.between?(min_i64, max_i64)
        raise InvalidValue.new(field.name, 'timestamp outside int64')
      end

      secs, nanos = value.divmod(1_000_000_000)
      txt = Time.at(secs, in: 'UTC').strftime('%Y-%m-%dT%H:%M:%S')
      txt = "#{txt}.#{nanos.to_s.rjust(9, '0').sub(/0+\z/, '')}" if nanos.positive?
      json_quote("#{txt}Z")
    end

    def self.read_json_fixed(field, token)
      str = if WIDE_FIXED_KINDS[field.kind]
              raise Refused.new('kind_mismatch', field.name) unless token.instance_of?(String)

              token
            else
              raise Refused.new('kind_mismatch', field.name) unless token.is_a?(JsonNumber)

              token.token
            end
      m = DECIMAL_RE.match(str)
      raise Refused.new('kind_mismatch', field.name) if m.nil?

      frac = m[3] || ''
      exponent = m[4] || '0'
      digits = m[2] + frac
      if digits.delete('0').empty?
        raise Refused.new('out_of_range', field.name) if field.lo.positive? || field.hi.negative?

        return 0
      end
      exp_digits = exponent.sub(/\A[+-]?0*/, '')
      if exp_digits.length > 6
        raise Refused.new(exponent.start_with?('-') ? 'kind_mismatch' : 'out_of_range', field.name)
      end

      exp = (exp_digits.empty? ? 0 : exp_digits.to_i) * (exponent.start_with?('-') ? -1 : 1)
      point = m[2].length + exp
      stripped = digits.sub(/\A0+/, '')
      point -= (digits.length - stripped.length)
      digits = stripped.sub(/0+\z/, '')
      raise Refused.new('out_of_range', field.name) if point > 40
      raise Refused.new('kind_mismatch', field.name) if point < -40 || digits.length > 200

      raw = digits.to_i << field.frac_bits
      scale = point - digits.length
      if scale >= 0
        raw *= (10**scale)
      else
        raw, rem = raw.divmod(10**(-scale))
        raise Refused.new('kind_mismatch', field.name) unless rem.zero?
      end
      raw = -raw unless m[1].empty?
      raise Refused.new('out_of_range', field.name) unless raw.between?(field.lo, field.hi)

      raw
    end

    def self.format_json_fixed(value, frac)
      neg = value.negative?
      whole, rest = value.abs.divmod(1 << frac)
      digits = rest.positive? ? (rest * (5**frac)).to_s.rjust(frac, '0').sub(/0+\z/, '') : '0'
      "#{'-' if neg}#{whole}.#{digits}"
    end

    def self.read_json_integer(field, token)
      str = if WIDE_INTEGER_KINDS[field.kind]
              raise Refused.new('kind_mismatch', field.name) unless token.instance_of?(String)

              token
            else
              raise Refused.new('kind_mismatch', field.name) unless token.is_a?(JsonNumber)

              token.token
            end
      raise Refused.new('kind_mismatch', field.name) if str.empty? || str == '-0' || str == '-'

      digits = u(str.start_with?('-') ? str[1..] : str)
      if digits.empty? || (digits.length > 1 && digits.start_with?('0')) || !/\A[0-9]+\z/.match?(digits)
        raise Refused.new('kind_mismatch', field.name)
      end
      raise Refused.new('out_of_range', field.name) if digits.length > 39

      val = str.to_i
      raise Refused.new('out_of_range', field.name) unless val.between?(field.lo, field.hi)

      val
    end

    def self.json_inner_field(field)
      inner = field.dup
      inner.codec = field.inner
      inner.element = field.elem unless field.elem.nil?
      inner
    end

    def self.json_array_element(field)
      if field.codec == 'pointer_array'
        el = field.dup
        el.codec = 'pointer'
        return el
      end
      if field.codec == 'union_array'
        el = field.dup
        el.codec = 'union'
        return el
      end
      raise InvalidValue.new(field.name, 'array element is unsupported') if field.element.nil?

      field.element
    end

    def self.json_entry_fields(field)
      cls = by_name(field.target)
      raise InvalidValue.new(field.name, "the table #{field.target} is not registered") if cls.nil?

      fields = cls.descriptor.fields
      [fields[0], fields[1]]
    end

    def self.from_json(cls, text, report = nil)
      data = parse_json_text(text, nil)
      val = cls.new
      desc = cls.descriptor
      graph = u(desc.variable ? SchemaTable.map_h : nil)
      unknown = [0]
      if desc.json_array
        raise Refused.new('json_root_not_array', desc.name) unless data.instance_of?(Array)

        f0 = desc.fields[0]
        f0.set(val, read_json_field(f0, data, graph, unknown))
      else
        raise Refused, 'kind_mismatch' unless data.instance_of?(Hash)

        fill_json_object(val, data, graph, unknown)
      end
      unless report.nil?
        report.unknown = unknown[0]
        report.widened = 0
        report.retained = 0
        report.retain_lost = 0
      end
      val
    end

    def self.fill_json_object(value, data, graph, unknown)
      desc = obj_class(value).descriptor
      fields_by_json = SchemaTable.map_h
      desc.fields.each { |f| fields_by_json[f.json_key] = f }
      data.each do |key, item|
        raise Refused, 'malformed' if key.include?("\x00") || key.bytesize > 255 || key.start_with?('&')

        field = fields_by_json[key]
        if field
          if field.codec == 'keyed'
            read_json_keyed(field, item, field.get(value), graph, unknown)
          else
            field.set(value, read_json_field(field, item, graph, unknown))
          end
        else
          unknown[0] += 1
          skip_json_unknown(item, graph)
        end
      end
      desc.fields.each do |field|
        if field.required && !data.key?(field.json_key) && field.guard_holds?(value)
          raise Refused.new('missing_required', field.name)
        end
      end
    end

    def self.skip_json_unknown(val, graph)
      if val.instance_of?(String)
        raise Refused, 'malformed' if val.include?("\x00")
      elsif val.instance_of?(Array)
        val.each { |item| skip_json_unknown(item, graph) }
      elsif val.instance_of?(Hash)
        val.each_with_index do |(key, item), idx|
          raise Refused, 'malformed' if key.include?("\x00") || key.bytesize > 255

          if !graph.nil? && key.start_with?('&')
            tok = u(item.is_a?(JsonNumber) ? item.token : nil)
            if idx != 0 || key != '&node' || tok.nil? || !/\A[1-9][0-9]*\z/.match?(tok) ||
               tok.length > 20 || tok.to_i > 0xFFFFFFFFFFFFFFFF
              raise Refused, 'malformed'
            end

            num = tok.to_i
            graph[num] ||= [nil, false]
          else
            skip_json_unknown(item, graph)
          end
        end
      end
    end

    def self.read_json_keyed(field, item, container, graph, unknown)
      raise Refused.new('kind_mismatch', field.name) if !item.instance_of?(Hash) || field.elem.nil?

      slots = field.keyed_list ? container : container._s_values
      item.each do |slot_name, cur|
        raise Refused.new('malformed', field.name) if slot_name.include?("\x00") || slot_name.bytesize > 255

        idx = field.json_keys.index(slot_name)
        if idx
          slots[idx] = read_json_field(field.elem, cur, graph, unknown)
        else
          unknown[0] += 1
          skip_json_unknown(cur, graph)
        end
      end
    end

    def self.read_json_field(field, val, graph, unknown)
      if %w[string_blob bytes_blob].include?(field.codec)
        return nil if val.nil?

        blob = field.dup
        blob.codec = field.codec == 'string_blob' ? 'string' : 'bytes'
        blob.size = 0xFFFFFFFF
        return read_json_field(blob, val, graph, unknown)
      end
      if field.codec == 'optional'
        return nil if val.nil?

        return read_json_field(json_inner_field(field), val, graph, unknown)
      end
      return read_json_float(field, val) if [10, 11].include?(field.kind) && !%w[array keyed map].include?(field.codec)

      if field.codec == 'union'
        raise Refused.new('kind_mismatch', field.name) if !val.instance_of?(Hash) || val.length > 1

        un = new_union_by_name(field.target)
        unless val.empty?
          name, item = val.first
          raise Refused, 'malformed' if name.include?("\x00") || name.bytesize > 255

          arm = obj_class(un).descriptor.arms.find { |a| a.name == name }
          if arm.nil?
            unknown[0] += 1
            skip_json_unknown(item, graph)
          elsif arm.field.nil?
            raise Refused.new('kind_mismatch', field.name) unless item.nil?

            un.type = arm.tag
          else
            un.type = arm.tag
            ivar_set(un, arm.ivar, read_json_field(arm.field, item, graph, unknown))
          end
        end
        return un
      end
      if field.json_flags && !%w[array pointer_array union_array map keyed].include?(field.codec)
        raise Refused.new('kind_mismatch', field.name) unless val.instance_of?(Array)

        res_flags = 0
        val.each do |name|
          raise Refused.new('kind_mismatch', field.name) unless name.instance_of?(String)
          raise Refused.new('length_over_bound', field.name) if name.bytesize > 255

          v_idx = field.json_variants.index(name)
          if v_idx
            bit = 1 << v_idx
            raise Refused.new('malformed', field.name) if res_flags.anybits?(bit)

            res_flags |= bit
          else
            unknown[0] += 1
          end
        end
        return res_flags
      end
      case field.codec
      when 'enum'
        raise Refused.new('kind_mismatch', field.name) unless val.instance_of?(String)
        raise Refused.new('length_over_bound', field.name) if val.bytesize > 255
        return 0 if val == 'None'

        v_idx = field.json_variants.index(val)
        if v_idx.nil?
          unknown[0] += 1
          return 0
        end
        ordinal = v_idx + 1
        field.deprecated[ordinal] ? field.default_value : ordinal
      when 'map'
        raise Refused.new('kind_mismatch', field.name) unless val.instance_of?(Hash)

        key_field, value_field = json_entry_fields(field)
        mapping = SchemaTable.map_h
        val.each do |k, item|
          raise Refused.new('text_holds_zero', field.name) if k.include?("\x00")

          parsed_key = if key_field.codec == 'string'
                         if !key_field.unbounded && k.bytesize > [255, key_field.size].min
                           raise Refused.new('length_over_bound', field.name)
                         end

                         k
                       elsif key_field.codec == 'int'
                         tok = WIDE_INTEGER_KINDS[key_field.kind] ? k : JsonNumber.new(k)
                         read_json_integer(key_field, tok)
                       else
                         raise Refused.new('unsupported', field.name)
                       end
          raise Refused.new('malformed', field.name) if mapping.key?(parsed_key)

          if value_field.codec == 'keyed'
            e_cls = by_name(field.target)
            raise Refused.new('unsupported', field.name) if e_cls.nil?

            container = value_field.get(e_cls.new)
            read_json_keyed(value_field, item, container, graph, unknown)
            mapping[parsed_key] = container
          else
            mapping[parsed_key] = read_json_field(value_field, item, graph, unknown)
          end
        end
        mapping
      when 'array', 'pointer_array', 'union_array'
        raise Refused.new('kind_mismatch', field.name) unless val.instance_of?(Array)

        element = json_array_element(field)
        raise Refused.new('count_over_bound', field.name) if field.array_kind != 3 && val.length > field.bound
        raise Refused.new('count_over_bound', field.name) if field.array_kind == 2 && val.length < field.array_min

        if field.json_text_list
          inner = element.dup
          inner.codec = 'string'
          result = SchemaTable.arr
          spent = 0
          val.each do |item|
            txt = u(element.codec == 'text_optional' && item.nil? ? nil : read_json_field(inner, item, graph, unknown))
            spent += txt.bytesize unless txt.nil?
            raise Refused.new('length_over_bound', field.name) if field.text_total.positive? && spent > field.text_total

            result << txt
          end
        else
          result = val.map { |item| read_json_field(element, item, graph, unknown) }
        end
        if field.array_kind == 1
          (field.bound - result.length).times { result << json_zero_element(element, field.name) }
        end
        result
      when 'pointer', 'table'
        return nil if field.codec == 'pointer' && val.nil?

        cls = by_name(field.target)
        raise Refused.new('kind_mismatch', field.name) if cls.nil?

        if cls.descriptor.json_array && field.codec == 'table'
          obj = cls.new
          f0 = cls.descriptor.fields[0]
          f0.set(obj, read_json_field(f0, val, graph, unknown))
          return obj
        end
        raise Refused.new('kind_mismatch', field.name) unless val.instance_of?(Hash)

        if field.codec == 'pointer' && !val.empty? && u(val.first)[0] == '&node'
          raise Refused, 'malformed' if graph.nil?

          lbl = val['&node']
          tok = u(lbl.is_a?(JsonNumber) ? lbl.token : nil)
          if tok.nil? || !/\A[1-9][0-9]*\z/.match?(tok) || tok.length > 20 || tok.to_i > 0xFFFFFFFFFFFFFFFF
            raise Refused, 'malformed'
          end

          num = tok.to_i
          if val.length == 1
            entry = graph[num]
            raise Refused, 'malformed' if entry.nil? || entry[1]
            raise Refused.new('kind_mismatch', field.name) if !entry[0].nil? && !obj_class(entry[0]).equal?(cls)

            return entry[0]
          end
          raise Refused, 'malformed' if graph.key?(num)

          obj = cls.new
          graph[num] = [obj, true]
          rest = val.except('&node')
          fill_json_object(obj, rest, graph, unknown)
          graph[num] = [obj, false]
          return obj
        end
        obj = cls.new
        fill_json_object(obj, val, graph, unknown)
        obj
      when 'bool'
        raise Refused.new('kind_mismatch', field.name) unless val.equal?(true) || val.equal?(false)

        val
      when 'int'
        return read_json_timestamp(field, val) if field.kind == 36
        return read_json_fixed(field, val) if FIXED_POINT_KINDS[field.kind]

        read_json_integer(field, val)
      when 'string'
        raise Refused.new('kind_mismatch', field.name) unless val.instance_of?(String)
        raise Refused.new('text_holds_zero', field.name) if val.include?("\x00")
        raise Refused.new('length_over_bound', field.name) if !field.unbounded && val.bytesize > field.size

        val
      when 'bytes'
        raw = decode_strict_base64(val, field.name)
        raise Refused.new('length_over_bound', field.name) if raw.bytesize > field.size

        raw
      else
        raise Refused.new('unsupported', field.name)
      end
    end

    def self.json_zero_element(element, field_name)
      case element.codec
      when 'table'
        cls = by_name(element.target)
        raise Refused.new('unsupported', field_name) if cls.nil?

        cls.new
      when 'pointer'
        nil
      when 'union'
        new_union_by_name(element.target)
      when 'bool'
        false
      when 'int', 'enum'
        0
      when 'float32', 'float64'
        0.0
      when 'string'
        ''
      when 'bytes'
        ''.b
      else
        return 0.0 if [10, 11].include?(element.kind)

        raise Refused.new('unsupported', field_name)
      end
    end

    def self.to_json(value)
      check_root_instance(value)
      counts = SchemaTable.map_h
      opened = { obj_id(value) => true }
      count_json_graph(value, counts, opened, 0)
      "#{write_json_table(value, 0, counts, SchemaTable.map_h, nil)}\n"
    end

    def self.count_json_graph(value, counts, opened, depth)
      desc = obj_class(value).descriptor
      raise InvalidValue.new(desc.name, 'nested past the depth cap') if depth > MAX_DEPTH

      desc.fields.each do |field|
        count_json_value(field, field.get(value), counts, opened, depth) if field.guard_holds?(value)
      end
    end

    def self.count_json_value(field, item, counts, opened, depth)
      if %w[string_blob bytes_blob].include?(field.codec) && item.is_a?(BlobRef)
        bid = obj_id(item)
        raise InvalidValue.new(field.name, 'blob_shared') if counts.key?(bid)

        counts[bid] = 1
        return
      end
      if field.codec == 'optional'
        return if item.nil?

        field = json_inner_field(field)
      end
      case field.codec
      when 'map'
        raise InvalidValue.new(field.name, 'not a map') unless item.instance_of?(Hash)

        key_field, value_field = json_entry_fields(field)
        item.each do |k, entry|
          write_json_scalar(key_field, k)
          count_json_value(value_field, entry, counts, opened, depth + 1)
        end
      when 'keyed'
        raise InvalidValue.new(field.name, 'keyed element is unsupported') if field.elem.nil?

        vals = if field.keyed_list
                 item.instance_of?(Array) ? item : nil
               else
                 item.is_a?(KeyedArray) ? item._s_values : nil
               end
        if vals.nil? || vals.length != field.json_keys.length
          raise InvalidValue.new(field.name, 'keyed array has an unnamed slot')
        end

        vals.each { |entry| count_json_value(field.elem, entry, counts, opened, depth + 1) }
      when 'union'
        un_cls = union_by_name(field.target)
        raise InvalidValue.new(field.name, 'not the declared union') if un_cls.nil? || !obj_class(item).equal?(un_cls)
        return if item.type.zero?

        arm = un_cls.descriptor.arms.find { |a| a.tag == item.type }
        raise InvalidValue.new(field.name, 'union tag names no arm') if arm.nil?

        count_json_value(arm.field, ivar_get(item, arm.ivar), counts, opened, depth + 1) if arm.field
      when 'array', 'pointer_array', 'union_array'
        element = json_array_element(field)
        raise InvalidValue.new(field.name, 'not a list') unless item.instance_of?(Array)
        if item.length > COUNT_CAP || (field.array_kind == 1 && item.length != field.bound) ||
           (field.array_kind == 2 && !item.length.between?(field.array_min, field.bound))
          raise InvalidValue.new(field.name, 'array count outside its bounds')
        end

        item.each { |entry| count_json_value(element, entry, counts, opened, depth + 1) } unless field.json_text_list
      when 'pointer', 'table'
        return if item.nil? && field.codec == 'pointer'

        cls = by_name(field.target)
        raise InvalidValue.new(field.name, 'not the declared table') if cls.nil? || !obj_class(item).equal?(cls)

        oid = obj_id(item)
        raise InvalidValue.new(field.name, 'cycle') if opened[oid]

        if field.codec == 'pointer'
          counts[oid] = (counts[oid] || 0) + 1
          return if counts[oid] > 1
        end
        opened[oid] = true
        count_json_graph(item, counts, opened, depth + 1)
        opened.delete(oid)
      end
    end

    def self.write_json_table(value, depth, counts, labels, label)
      desc = obj_class(value).descriptor
      raise InvalidValue.new(desc.name, 'nested past the depth cap') if depth > MAX_DEPTH

      if desc.json_array
        f0 = desc.fields[0]
        return render_json_value(f0, f0.get(value), depth, counts, labels)
      end
      lines = SchemaTable.arr
      indent = '  ' * (depth + 1)
      lines << "#{indent}\"&node\": #{label}" unless label.nil?
      desc.fields.each do |field|
        next unless field.guard_holds?(value)

        item = field.get(value)
        next if field.codec == 'optional' && item.nil?

        rendered = render_json_value(field, item, depth + 1, counts, labels)
        lines << "#{indent}#{json_quote(field.json_key)}: #{rendered}"
      end
      if !label.nil? && lines.length == 1
        raise InvalidValue.new(desc.name,
                               'a shared node with no field has no text spelling')
      end
      return '{}' if lines.empty?

      "{\n#{lines.join(",\n")}\n#{'  ' * depth}}"
    end

    def self.render_json_value(field, item, depth, counts, labels)
      structural = %w[pointer table array pointer_array union_array map keyed
                      union].include?(field.codec) || field.json_flags
      if depth > MAX_DEPTH && !(field.codec == 'pointer' && item.nil?) && structural
        raise InvalidValue.new(field.name, 'nested past the depth cap')
      end
      if field.codec == 'optional'
        return item.nil? ? 'null' : render_json_value(json_inner_field(field), item, depth, counts, labels)
      end

      if %w[pointer table].include?(field.codec)
        return 'null' if item.nil? && field.codec == 'pointer'

        oid = obj_id(item)
        if field.codec == 'pointer' && (counts[oid] || 0) > 1
          return "{\n#{'  ' * (depth + 1)}\"&node\": #{labels[oid]}\n#{'  ' * depth}}" if labels.key?(oid)

          labels[oid] = labels.length + 1
          return write_json_table(item, depth, counts, labels, labels[oid])
        end
        return write_json_table(item, depth, counts, labels, nil)
      end
      if %w[array pointer_array union_array].include?(field.codec)
        element = json_array_element(field)
        return '[]' if item.empty?

        if field.json_text_list
          parts = SchemaTable.arr
          spent = 0
          inner = element.dup
          inner.codec = 'string'
          item.each do |txt|
            rendered = element.codec == 'text_optional' && txt.nil? ? 'null' : write_json_scalar(inner, txt)
            unless txt.nil?
              utf8 = validate_text(txt, field.name, inner.unbounded ? 0 : inner.size)
              spent += utf8.bytesize
            end
            if field.text_total.positive? && spent > field.text_total
              raise InvalidValue.new(field.name,
                                     'text total exceeds its bound')
            end

            parts << "#{'  ' * (depth + 1)}#{rendered}"
          end
        else
          parts = item.map do |entry|
            "#{'  ' * (depth + 1)}#{render_json_value(element, entry, depth + 1, counts, labels)}"
          end
        end
        return "[\n#{parts.join(",\n")}\n#{'  ' * depth}]"
      end
      if field.codec == 'map'
        key_field, value_field = json_entry_fields(field)
        return '{}' if item.empty?

        parts = sorted_map_keys(field, item).map do |k|
          k_txt = key_field.codec == 'int' ? k.to_s : k
          "#{'  ' * (depth + 1)}#{json_quote(k_txt)}: #{render_json_value(value_field, item[k], depth + 1, counts,
                                                                          labels)}"
        end
        return "{\n#{parts.join(",\n")}\n#{'  ' * depth}}"
      end
      if field.codec == 'keyed'
        vals = field.keyed_list ? item : item._s_values
        raise InvalidValue.new(field.name, 'keyed element is unsupported') if field.elem.nil?
        return '{}' if field.json_keys.empty?

        parts = field.json_keys.each_with_index.map do |k_name, idx|
          "#{'  ' * (depth + 1)}#{json_quote(k_name)}: #{render_json_value(field.elem, vals[idx], depth + 1, counts,
                                                                           labels)}"
        end
        return "{\n#{parts.join(",\n")}\n#{'  ' * depth}}"
      end
      if field.codec == 'union'
        return '{}' if item.type.zero?

        udesc = obj_class(item).descriptor
        arm = udesc.arms.find { |a| a.tag == item.type }
        raise InvalidValue.new(field.name, 'union tag names no arm') if arm.nil?

        part = if arm.field.nil?
                 'null'
               else
                 render_json_value(arm.field, ivar_get(item, arm.ivar), depth + 1, counts,
                                   labels)
               end
        return "{\n#{'  ' * (depth + 1)}#{json_quote(arm.name)}: #{part}\n#{'  ' * depth}}"
      end
      if field.json_flags
        if !item.instance_of?(Integer) || item.negative? || (item >> field.json_variants.length).positive?
          raise InvalidValue.new(field.name, 'flags name no variant')
        end

        parts = SchemaTable.arr
        field.json_variants.each_with_index do |v_name, idx|
          parts << "#{'  ' * (depth + 1)}#{json_quote(v_name)}" if item.anybits?(1 << idx)
        end
        return '[]' if parts.empty?

        return "[\n#{parts.join(",\n")}\n#{'  ' * depth}]"
      end
      write_json_scalar(field, item)
    end

    def self.write_json_scalar(field, val)
      if %w[string_blob bytes_blob].include?(field.codec)
        return 'null' if val.nil?

        raw_val = val.is_a?(BlobRef) ? val.value : val
        blob = field.dup
        blob.codec = field.codec == 'string_blob' ? 'string' : 'bytes'
        blob.size = 0xFFFFFFFF
        return write_json_scalar(blob, raw_val)
      end
      return write_json_scalar(json_inner_field(field), val) if field.codec == 'optional'
      return write_json_float(field, val) if [10, 11].include?(field.kind)

      case field.codec
      when 'enum'
        if !val.instance_of?(Integer) || !val.between?(0, field.json_variants.length) || field.deprecated[val]
          raise InvalidValue.new(field.name, 'enum value names no writable variant')
        end

        json_quote(val.zero? ? 'None' : field.json_variants[val - 1])
      when 'bool'
        validate_bool(val, field.name)
        val ? 'true' : 'false'
      when 'int'
        validate_int(val, field.name, field.lo, field.hi)
        return write_json_timestamp(field, val) if field.kind == 36

        if FIXED_POINT_KINDS[field.kind]
          tok = format_json_fixed(val, field.frac_bits)
          return WIDE_FIXED_KINDS[field.kind] ? "\"#{tok}\"" : tok
        end
        tok = val.to_s
        WIDE_INTEGER_KINDS[field.kind] ? "\"#{tok}\"" : tok
      when 'string'
        utf8 = validate_text(val, field.name, field.unbounded ? 0 : field.size)
        json_quote(utf8)
      when 'bytes'
        raw = validate_bytes(val, field.name, field.size)
        json_quote([raw].pack('m0'))
      else
        raise InvalidValue.new(field.name, 'unsupported JSON field')
      end
    end
  end
end
