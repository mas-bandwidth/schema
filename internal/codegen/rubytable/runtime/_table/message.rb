# frozen_string_literal: true

module SchemaRuntimeUnit
  # Strict connection refusal when remote build_version differs from local.
  class BuildVersionMismatch < Refused
    attr_reader :remote, :own

    def initialize(remote, own)
      super('build_version_mismatch')
      @remote = remote
      @own = own
    end
  end

  # Form-2 bit-packed message vocabulary and codec (SPEC-TABLES §3.3).
  module SchemaTable
    # Bit-level LSB-first reader over an ASCII-8BIT batch.
    class MsgBitReader
      attr_reader :data
      attr_accessor :position

      def initialize(data, position = 0)
        @data = data
        @position = position
      end

      def get(width)
        if width.negative? || @position + width > @data.bytesize * 8
          raise Refused.new('wire_damaged', 'message bit stream is truncated')
        end
        return 0 if width.zero?

        start = @position >> 3
        shift = @position & 7
        end_byte = (@position + width + 7) >> 3
        val = SchemaTable.unpack_int_le(@data, start, end_byte - start, false)
        @position += width
        (val >> shift) & ((1 << width) - 1)
      end

      def align
        padding = (-@position) & 7
        raise Refused.new('wire_damaged', 'message padding is nonzero') unless get(padding).zero?
      end

      def raw(len)
        rem_bytes = ((@data.bytesize * 8) - @position) >> 3
        if @position.anybits?(7) || len.negative? || len > rem_bytes
          raise Refused.new('wire_damaged', 'message bytes exceed their buffer')
        end

        start = @position >> 3
        @position += len * 8
        @data.byteslice(start, len).b
      end

      def finish
        align
        raise Refused.new('wire_damaged', 'bytes follow the message batch') if @position != @data.bytesize * 8
      end
    end

    # Bit-level LSB-first writer across a message batch.
    class MsgBitWriter
      attr_reader :data, :position, :measure

      def initialize(measure: false)
        @data = String.new(encoding: Encoding::ASCII_8BIT)
        @position = 0
        @measure = measure
      end

      def put(val, width)
        if width.negative? || val.negative? || val.bit_length > width
          raise ArgumentError,
                'value does not fit the message width'
        end
        return if width.zero?

        unless @measure
          start = @position >> 3
          shift = @position & 7
          end_byte = (@position + width + 7) >> 3
          @data << ("\x00".b * (end_byte - @data.bytesize)) if end_byte > @data.bytesize
          if end_byte > start
            cur = SchemaTable.unpack_int_le(@data, start, end_byte - start, false)
            @data[start, end_byte - start] = SchemaTable.pack_int_le(cur | (val << shift), end_byte - start, false)
          end
        end
        @position += width
      end

      def align
        put(0, (-@position) & 7)
      end

      def raw(bytes)
        raise ArgumentError, 'message bytes are not aligned' if @position.anybits?(7)

        @data << bytes unless @measure
        @position += bytes.bytesize * 8
      end

      def finish
        align
        @data
      end
    end

    # Announced field/element shape from a remote endpoint.
    class MsgShape
      attr_accessor :kind, :packing, :bits, :base, :minimum, :maximum, :element, :inner, :quant

      def initialize(kind)
        @kind = kind
        @packing = 0
        @bits = 0
        @base = 0
        @minimum = 0
        @maximum = 0
        @element = 0
        @inner = nil
        @quant = nil
      end
    end

    MsgEntry = Struct.new(:field_id, :kind, :shape, :key)

    # Terminal announcement state and resolved vocabulary for one form-2 connection.
    class MessageVocabulary
      attr_accessor :strict, :own_version, :build_version, :announced, :refused,
                    :entries, :unknown, :resolved, :slots, :names, :variants

      def initialize(own_version = 0, strict: false)
        @strict = strict
        @own_version = own_version
        @build_version = 0
        @announced = false
        @refused = false
        @entries = SchemaTable.arr
        @unknown = 0
        @resolved = SchemaTable.map_h
        @slots = SchemaTable.map_h
        @names = SchemaTable.map_h
        @variants = SchemaTable.map_h
      end

      def ref_bits
        bits = @entries.length.bit_length
        [bits, 1].max
      end
    end

    # Synthetic root holder used while parsing an announcement frame.
    class MsgAnnouncementRoot
      DESC = TableDesc.new(name: '_announcement', type_id: 0, variable: true, fields: SchemaTable::EMPTY_ARR)

      def self.descriptor
        DESC
      end
    end

    def self.msg_kind_bits(kind)
      return 8 << ((kind - 2) % 4) if kind.between?(2, 9)
      return 128 if [18, 19].include?(kind)
      return 8 << ((kind - 20) % 5) if kind.between?(20, 29)

      raise Refused.new('wire_damaged', 'unknown numeric kind')
    end

    def self.msg_known_kind?(kind)
      kind.between?(0, 32) || kind == 36
    end

    def self.msg_parse_shape(reader, kind)
      shape = MsgShape.new(kind)
      if kind.between?(2, 10) || kind.between?(18, 29)
        shape.packing = reader.byte
        return shape if shape.packing.zero?

        if kind == 10 && shape.packing == 2
          raw12 = reader.take(12)
          min_v, max_v, res = raw12.unpack('eee')
          unless min_v.finite? && max_v.finite? && res.finite? && min_v < max_v && res.positive?
            raise Refused.new('wire_damaged', 'invalid compressed float shape')
          end

          delta = float32_round(max_v - min_v)
          steps = float32_round(delta / res)
          raise Refused.new('wire_damaged', 'invalid compressed float width') unless delta.finite? && steps.finite?

          clamped = steps.clamp(1.0, COMPRESSED_FLOAT_VALUES_MAX)
          count = clamped.ceil
          shape.bits = count.bit_length
          shape.quant = [min_v, max_v, res].freeze
          return shape
        end
        raise Refused.new('wire_damaged', 'invalid numeric packing') if shape.packing != 1 || kind == 10

        width = reader.varint
        domain = msg_kind_bits(kind)
        raise Refused.new('wire_damaged', 'message width exceeds its kind') if width > domain

        shape.bits = width
        signed = kind.between?(2, 5) || kind == 18 || kind.between?(20, 24)
        if kind == 18 || kind == 19 || kind.between?(20, 29)
          shape.base = unpack_int_le(reader.take(16), 0, 16, signed)
        else
          raw = reader.varint
          shape.base = signed ? (raw >> 1) ^ -(raw & 1) : raw
        end
        low = signed ? -(1 << (domain - 1)) : 0
        high = (1 << (domain - (signed ? 1 : 0))) - 1
        raise Refused.new('wire_damaged', 'message base exceeds its kind') unless shape.base.between?(low, high)

        return shape
      end
      if kind == 12
        shape.maximum = reader.varint
        raise Refused.new('wire_damaged', 'message text bound exceeds storage') if shape.maximum > COUNT_CAP
      elsif [14, 16].include?(kind)
        shape.minimum = reader.varint if kind == 14
        shape.maximum = reader.varint
        if shape.minimum > shape.maximum || shape.maximum > 0xFFFFFFFF
          raise Refused.new('wire_damaged', 'invalid message array bound')
        end

        shape.element = reader.byte
        if !msg_known_kind?(shape.element) || shape.element == 14 || shape.element == 16
          raise Refused.new('wire_damaged', 'invalid message element kind')
        end

        shape.inner = msg_parse_shape(reader, shape.element)
      end
      shape
    end

    def self.announce_read_raw(vocab, data, max_bytes: 65_536, max_entries: 4096)
      raise Refused, 'second_announcement' if vocab.announced || vocab.refused

      vocab.refused = true
      raw = binary_input(data)
      raise Refused.new('wire_damaged', 'announcement has no form byte') if raw.empty?

      first = raw.getbyte(0)
      raise Refused, first == 2 ? 'message_form_as_file' : 'newer_form' if first != 1

      ids = parse_id_table(raw)
      check_id_order(raw, ids, announcement: true)
      reader = Reader.new(raw, 1, body_end_offset(raw), ids)
      ctx = LoadCtx.new(ids)
      ctx.nonfinite = SchemaTable.arr
      ctx.root = MsgAnnouncementRoot.new
      seen = SchemaTable.map_h
      raw_vocab = u(nil)
      version_seen = false
      unknown = 0
      oversize = false
      loop do
        ref = reader.varint
        break if ref.zero?

        field_id = reader.id_at(ref)
        kind = reader.byte
        raise Refused.new('wire_damaged', 'announcement field repeated') if seen[field_id]

        seen[field_id] = true
        if field_id == BUILD_VERSION_WIRE_ID
          raise Refused.new('wire_damaged', 'announcement build version has wrong kind') if kind != 9

          vocab.build_version = unpack_int_le(reader.take(8), 0, 8, false)
          version_seen = true
        elsif field_id == MESSAGE_VOCABULARY_WIRE_ID
          raise Refused.new('wire_damaged', 'announcement vocabulary has wrong kind') if kind != 14

          field_bytes = reader.take(reader.varint)
          content = Reader.new(field_bytes, 0, field_bytes.bytesize, ids)
          raise Refused.new('wire_damaged', 'announcement vocabulary is not bytes') if content.byte != 6

          len = content.varint
          if len != content.end_pos - content.pos
            raise Refused.new('wire_damaged',
                              'announcement vocabulary has invalid length')
          end

          oversize = len > max_bytes
          raw_vocab = content.take(len)
        else
          if field_id == NODE_WIRE_ID
            raise Refused.new('wire_damaged', 'announcement node table has wrong kind') if kind != 12

            payload = reader.take(reader.varint)
            chk = LoadCtx.new(ids)
            chk.nonfinite = ctx.nonfinite
            check_node_table_wire(payload, chk)
            read_node_table(payload, ctx)
          else
            skip_payload(reader, kind, ctx, ctx.depth)
          end
          unknown += 1
        end
      end
      if reader.pos != reader.end_pos || !version_seen || raw_vocab.nil?
        raise Refused.new('wire_damaged', 'incomplete announcement')
      end

      resolve_graph(ctx)
      raise Refused, 'non_finite_float' unless ctx.nonfinite.empty?
      raise Refused, 'vocabulary_too_large' if oversize
      if vocab.strict && vocab.build_version != vocab.own_version
        raise BuildVersionMismatch.new(vocab.build_version,
                                       vocab.own_version)
      end

      source = Reader.new(raw_vocab, 0, raw_vocab.bytesize, SchemaTable.arr)
      entries = SchemaTable.arr
      triples = SchemaTable.map_h
      node_seen = false
      while source.pos < source.end_pos
        raise Refused, 'vocabulary_too_large' if entries.length >= max_entries

        start = source.pos
        field_id = unpack_int_le(source.take(8), 0, 8, false)
        kind = source.byte
        if field_id == BUILD_VERSION_WIRE_ID || field_id == MESSAGE_VOCABULARY_WIRE_ID || !msg_known_kind?(kind)
          raise Refused.new('wire_damaged', 'invalid message vocabulary entry')
        end

        if field_id == NODE_WIRE_ID
          raise Refused.new('wire_damaged', 'node vocabulary entry repeated') if node_seen

          node_seen = true
        end
        shape = msg_parse_shape(source, kind)
        key = raw_vocab.byteslice(start, source.pos - start).b
        raise Refused.new('wire_damaged', 'message vocabulary entry repeated') if triples[key]

        triples[key] = true
        entries << MsgEntry.new(field_id, kind, shape, key)
      end
      slots = SchemaTable.map_h
      names = SchemaTable.map_h
      entries.each_with_index do |entry, i|
        slots[entry.key] = i + 1
        names[entry.field_id] = i + 1 if entry.kind.zero?
      end
      vocab.entries = entries
      vocab.slots = slots
      vocab.names = names
      vocab.unknown = unknown
      vocab.announced = true
      vocab.refused = false
    end

    def self.resolve_vocabulary(vocab, _local, records)
      resolved = SchemaTable.map_h
      variants = SchemaTable.map_h
      visited = SchemaTable.map_h
      collect_fn = u(nil)
      collect_fn = lambda do |field|
        fid = obj_id(field)
        return if visited[fid]

        visited[fid] = true
        unless field.variant_ids.empty?
          vmap = SchemaTable.map_h
          field.variant_ids.each_with_index { |wid, idx| vmap[wid] = idx + 1 }
          variants[fid] = vmap
        end
        collect_fn.call(field.elem) if field.elem
        collect_fn.call(field.element) if field.element
        un_cls = u(field.target.empty? ? nil : union_by_name(field.target))
        if un_cls
          un_cls.descriptor.arms.each do |arm|
            collect_fn.call(arm.field) if arm.field
          end
        end
      end

      records.each do |rec_name, cls|
        cls.descriptor.fields.each do |field|
          resolved[[rec_name, field.field_id]] = field
          collect_fn.call(field)
        end
      end
      vocab.resolved = resolved
      vocab.variants = variants
    end

    def self.msg_reference(reader, vocab)
      ref = reader.get(vocab.ref_bits)
      return nil if ref.zero?
      raise Refused.new('wire_damaged', 'message reference exceeds vocabulary') if ref > vocab.entries.length

      entry = vocab.entries[ref - 1]
      raise Refused.new('wire_damaged', 'reserved message reference') if entry.field_id >= 0xFFFFFFFFFFFFFFFD

      entry
    end

    def self.msg_numeric_width(shape)
      return 1 if shape.kind == 1
      return 64 if shape.kind == 36

      shape.packing == 1 ? shape.bits : msg_kind_bits(shape.kind)
    end

    def self.msg_write_scalar(writer, shape, value)
      if shape.kind == 1
        validate_bool(value, '')
        writer.put(value ? 1 : 0, 1)
        return
      end
      if [10, 11].include?(shape.kind)
        raise InvalidValue.new('', 'message float is not finite') unless value.instance_of?(Float) && value.finite?

        if shape.packing == 2
          min_v, max_v, res = shape.quant
          v = float32_round(value)
          raise InvalidValue.new('', 'compressed float exceeds its declared range') unless v.between?(min_v, max_v)

          delta = float32_round(max_v - min_v)
          count = float32_round(delta / res).clamp(1.0, COMPRESSED_FLOAT_VALUES_MAX).ceil
          norm = float32_round(float32_round(v - min_v) / delta).clamp(0.0, 1.0)
          idx = float32_round(float32_round(norm * float32_round(count.to_f)) + 0.5).floor
          idx = count if idx > count
          writer.put(idx, shape.bits)
        elsif shape.kind == 10
          r = finite_float32(value, '')
          writer.put([r].pack('e').unpack1('V'), 32)
        else
          writer.put([value].pack('E').unpack1('Q<'), 64)
        end
        return
      end
      unless shape.kind.between?(2, 9) || shape.kind.between?(18, 29) || shape.kind == 36
        raise InvalidValue.new('', 'message kind is not implemented')
      end
      raise InvalidValue.new('', 'message integer is not int') unless value.instance_of?(Integer)

      width = msg_numeric_width(shape)
      raw = shape.packing == 1 ? value - shape.base : value & ((1 << width) - 1)
      if raw.negative? || raw.bit_length > width
        raise InvalidValue.new('',
                               'message value is outside its announced width')
      end

      writer.put(raw, width)
    end

    def self.msg_read_scalar(reader, shape)
      return !reader.get(1).zero? if shape.kind == 1

      if [10, 11].include?(shape.kind)
        if shape.packing == 2
          min_v, max_v, res = shape.quant
          delta = float32_round(max_v - min_v)
          count = float32_round(delta / res).clamp(1.0, COMPRESSED_FLOAT_VALUES_MAX).ceil
          idx = reader.get(shape.bits)
          raise Refused.new('wire_damaged', 'compressed float index exceeds count') if idx > count

          frac = float32_round(float32_round(idx.to_f) / float32_round(count.to_f))
          val = float32_round(float32_round(frac * delta) + min_v)
        elsif shape.kind == 10
          bits = reader.get(32)
          raise Refused, 'non_finite_float' if (bits >> 23).allbits?(0xFF)

          val = [bits].pack('V').unpack1('e')
        else
          bits = reader.get(64)
          raise Refused, 'non_finite_float' if (bits >> 52).allbits?(0x7FF)

          val = [bits].pack('Q<').unpack1('E')
        end
        raise Refused, 'non_finite_float' unless val.finite?

        return val
      end
      unless shape.kind.between?(2, 9) || shape.kind.between?(18, 29) || shape.kind == 36
        raise Refused.new('kind_mismatch', 'message kind is not implemented')
      end

      width = msg_numeric_width(shape)
      val = reader.get(width)
      return val + shape.base if shape.packing == 1

      signed = shape.kind.between?(2, 5) || shape.kind == 18 || shape.kind.between?(20, 24) || shape.kind == 36
      signed && width.positive? && val.anybits?(1 << (width - 1)) ? val - (1 << width) : val
    end

    def self.msg_name_slot(vocab, field_id)
      slot = vocab.names[field_id]
      raise ArgumentError, 'message vocabulary is missing a name' if slot.nil?

      slot
    end

    def self.msg_write_body(writer, obj, vocab, keys)
      desc = obj_class(obj).descriptor
      desc.fields.each do |field|
        cur = field.get(obj)
        next if field.default?(cur) && !field.required

        key = keys[[desc.name, field.name]]
        slot = u(key ? vocab.slots[key] : nil)
        raise ArgumentError, 'message field is absent from its own vocabulary' if slot.nil?

        writer.put(slot, vocab.ref_bits)
        msg_write_value(writer, field, cur, vocab.entries[slot - 1].shape, vocab, keys)
      end
      writer.put(0, vocab.ref_bits)
    end

    def self.msg_write_value(writer, fd, val, shape, vocab, keys)
      case shape.kind
      when 13
        msg_write_body(writer, val, vocab, keys)
      when 12
        utf8 = validate_text(val, fd.name, 0).b
        raise InvalidValue.new(fd.name, 'message text is invalid') if utf8.bytesize > shape.maximum

        writer.put(utf8.bytesize, shape.maximum.bit_length)
        writer.align
        writer.raw(utf8)
      when 14
        if !val.length.between?(shape.minimum, shape.maximum) || shape.inner.nil?
          raise InvalidValue.new(fd.name, 'message array count exceeds its bound')
        end

        writer.put(val.length - shape.minimum, (shape.maximum - shape.minimum).bit_length)
        writer.align if shape.element == 6
        if fd.codec == 'bytes' && shape.element == 6 && shape.inner.packing.zero?
          writer.raw(val)
          return
        end
        elem = fd.elem || fd.element || fd
        if fd.codec == 'bytes'
          val.each_byte { |b| msg_write_value(writer, elem, b, shape.inner, vocab, keys) }
        else
          val.each { |item| msg_write_value(writer, elem, item, shape.inner, vocab, keys) }
        end
      when 30
        slot = val.zero? ? 0 : msg_name_slot(vocab, fd.variant_ids[val - 1])
        writer.put(slot, vocab.ref_bits)
      when 15
        if val.type.zero?
          writer.put(0, vocab.ref_bits)
          return
        end
        udesc = obj_class(val).descriptor
        arm = udesc.arms[val.type - 1]
        key = keys[[udesc.name, arm.name]]
        arm_slot = u(key ? vocab.slots[key] : nil)
        raise ArgumentError, 'message union arm is absent from its vocabulary' if arm_slot.nil?

        writer.put(arm_slot, vocab.ref_bits)
        if arm.field
          msg_write_value(writer, arm.field, ivar_get(val, arm.ivar), vocab.entries[arm_slot - 1].shape, vocab, keys)
        end
      when 16
        raise InvalidValue.new(fd.name, 'message keyed element is missing') if fd.elem.nil? || shape.inner.nil?

        vals = fd.keyed_list ? val : val._s_values
        present = SchemaTable.arr
        fd.keys.each_with_index do |k, idx|
          present << [k, vals[idx]] unless fd.elem.default?(vals[idx])
        end
        raise InvalidValue.new(fd.name, 'message keyed count exceeds its bound') if present.length > shape.maximum

        writer.put(present.length, shape.maximum.bit_length)
        present.each do |k, item|
          writer.put(msg_name_slot(vocab, k), vocab.ref_bits)
          msg_write_value(writer, fd.elem, item, shape.inner, vocab, keys)
        end
      else
        msg_write_scalar(writer, shape, val)
      end
    end

    def self.msg_read_body(reader, cls, vocab, own_version, keys, stats, depth = 0)
      raise Refused.new('wire_damaged', 'message nesting exceeds 128') if depth > MAX_DEPTH

      obj = cls.new
      desc = cls.descriptor
      seen = SchemaTable.map_h
      last_ordinal = -1
      loop do
        entry = msg_reference(reader, vocab)
        if entry.nil?
          desc.fields.each do |field|
            raise Refused.new('missing_required', field.name) if field.required && !seen[field.field_id]
          end
          return obj
        end
        raise Refused.new('wire_damaged', 'message field repeated') if seen[entry.field_id]

        seen[entry.field_id] = true
        field = vocab.resolved[[desc.name, entry.field_id]]
        if field.nil?
          if vocab.build_version == own_version
            raise Refused.new('wire_damaged', 'same-build message names an undeclared field')
          end

          msg_skip_value(reader, entry.shape, vocab, depth)
          stats[0] += 1
          next
        end
        own_key = keys[[desc.name, field.name]]
        if entry.kind != field.kind
          raise Refused.new('kind_mismatch', 'message field kind changed') unless widens?(entry.kind, field.kind)

          stats[1] += 1
        end
        same_shape = vocab.build_version == own_version && own_key == entry.key
        if same_shape
          raise Refused.new('wire_damaged', 'message fields are out of order') if field.ordinal <= last_ordinal

          last_ordinal = field.ordinal
        end
        substituted = stats[2]
        val = msg_read_value(reader, field, entry.shape, vocab, own_version, keys, stats, depth,
                             in_field: true, existing: field.get(obj), same_shape: same_shape)
        raise Refused.new('out_of_range', field.name) if val.instance_of?(Integer) && !val.between?(field.lo, field.hi)
        if val.instance_of?(Float) && field.float_range && !val.between?(field.fmin, field.fmax)
          raise Refused.new('out_of_range', field.name)
        end
        if same_shape && stats[2] == substituted && field.default?(val) && !field.required
          raise Refused.new('wire_damaged', 'message field is explicitly at its default')
        end

        field.set(obj, val)
      end
    end

    def self.msg_read_value(reader, fd, shape, vocab, own_version, keys, stats, depth,
                            in_field: false, existing: nil, same_shape: false)
      case shape.kind
      when 13
        cls = by_name(fd.target)
        raise Refused.new('kind_mismatch', 'message table target is not registered') if cls.nil?

        return msg_read_body(reader, cls, vocab, own_version, keys, stats, depth + 1)
      when 12
        len = reader.get(shape.maximum.bit_length)
        raise Refused.new('wire_damaged', 'message text exceeds announced bound') if len > shape.maximum
        raise Refused.new('length_over_bound', fd.name) if len > fd.size

        reader.align
        raw = reader.raw(len)
        return decode_wire_text(raw, 'message text is invalid')
      when 14
        count = shape.minimum + reader.get((shape.maximum - shape.minimum).bit_length)
        raise Refused.new('wire_damaged', 'message count exceeds announced bound') if count > shape.maximum

        bound = fd.codec == 'bytes' ? fd.size : fd.bound
        if count > bound
          raise Refused.new(fd.codec == 'bytes' ? 'length_over_bound' : 'count_over_bound', fd.name)
        end

        reader.align if shape.element == 6
        raise Refused.new('wire_damaged', 'message element shape is absent') if shape.inner.nil?
        return reader.raw(count) if fd.codec == 'bytes' && shape.element == 6 && shape.inner.packing.zero?

        elem = fd.elem || fd.element || fd
        if !elem.equal?(fd) && shape.element != elem.kind
          raise Refused.new('kind_mismatch', 'message array element kind changed') unless widens?(shape.element,
                                                                                                  elem.kind)

          stats[1] += 1
        end
        raise Refused.new('count_over_bound', fd.name) if fd.array_kind == 2 && count < fd.array_min

        values = Array.new(count) do
          msg_read_value(reader, elem, shape.inner, vocab, own_version, keys, stats, depth + 1)
        end
        if fd.array_kind == 1 && count < fd.bound
          (fd.bound - count).times { values << born_element(fd, elem) }
        end
        return fd.codec == 'bytes' ? values.pack('C*') : values
      when 30
        entry = msg_reference(reader, vocab)
        return 0 if entry.nil?
        raise Refused.new('wire_damaged', 'message enum name has a payload') unless entry.kind.zero?

        vmap = vocab.variants[obj_id(fd)]
        ordinal = vmap ? vmap[entry.field_id] : nil
        unless ordinal.nil?
          if fd.deprecated[ordinal]
            stats[2] += 1
            return fd.default_value
          end
          return ordinal
        end
        stats[0] += 1
        stats[2] += 1
        return 0
      when 15
        cls = union_by_name(fd.target)
        raise Refused.new('kind_mismatch', 'message union target is not registered') if cls.nil?

        res = cls.new
        entry = msg_reference(reader, vocab)
        if entry.nil?
          raise Refused.new('wire_damaged', 'a union field explicitly carries None') if in_field

          return res
        end
        raise Refused.new('wire_damaged', 'message union arm has no framing') if entry.kind.zero?

        arm = cls.descriptor.by_id[entry.field_id]
        if arm.nil?
          msg_skip_value(reader, entry.shape, vocab, depth + 1)
          stats[0] += 1
          stats[2] += 1
          return res
        end
        if entry.kind != arm.kind
          raise Refused.new('kind_mismatch', 'message union arm kind changed') unless widens?(entry.kind, arm.kind)

          stats[1] += 1
        end
        res.type = arm.tag
        if arm.field
          arm_val = msg_read_value(reader, arm.field, entry.shape, vocab, own_version, keys, stats, depth + 1)
          ivar_set(res, arm.ivar, arm_val)
        end
        return res
      when 16
        if shape.inner.nil? || fd.elem.nil? || existing.nil?
          raise Refused.new('wire_damaged',
                            'message keyed element is missing')
        end
        raise Refused.new('kind_mismatch', 'message keyed element kind changed') if shape.element != fd.elem.kind

        count = reader.get(shape.maximum.bit_length)
        raise Refused.new('wire_damaged', 'message keyed count exceeds its bound') if count > shape.maximum

        slots = fd.keyed_list ? existing : existing._s_values
        seen = SchemaTable.map_h
        last_idx = -1
        count.times do
          key = msg_reference(reader, vocab)
          if key.nil? || !key.kind.zero? || seen[key.field_id]
            raise Refused.new('wire_damaged', 'invalid or repeated message key')
          end

          seen[key.field_id] = true
          idx = fd.keys.index(key.field_id)
          if idx.nil?
            msg_skip_value(reader, shape.inner, vocab, depth + 1)
            stats[0] += 1
            stats[2] += 1
            next
          end
          raise Refused.new('wire_damaged', 'message keyed slots are out of order') if same_shape && idx <= last_idx

          last_idx = idx
          substituted = stats[2]
          item = msg_read_value(reader, fd.elem, shape.inner, vocab, own_version, keys, stats, depth + 1)
          if same_shape && stats[2] == substituted && fd.elem.default?(item)
            raise Refused.new('wire_damaged', 'message keyed slot is explicitly at its default')
          end

          slots[idx] = item
        end
        return existing
      end

      val = msg_read_scalar(reader, shape)
      if val.instance_of?(Integer) && fd.codec != 'bytes' && !val.between?(fd.lo, fd.hi)
        raise Refused.new('out_of_range', fd.name)
      end
      if val.instance_of?(Float) && fd.float_range && !val.between?(fd.fmin, fd.fmax)
        raise Refused.new('out_of_range', fd.name)
      end

      val
    end

    def self.msg_skip_value(reader, shape, vocab, depth, in_field: false, pending: nil)
      raise Refused.new('wire_damaged', 'message nesting exceeds 128') if depth > MAX_DEPTH

      kind = shape.kind
      return if kind.zero? || kind == 32

      if kind == 1 || kind.between?(2, 9) || kind.between?(18, 29) || kind == 36
        val = msg_read_scalar(reader, shape)
        if val.instance_of?(Integer)
          width = kind == 36 ? 64 : msg_kind_bits(kind)
          signed = kind.between?(2, 5) || kind == 18 || kind.between?(20, 24) || kind == 36
          low = signed ? -(1 << (width - 1)) : 0
          high = (1 << (width - (signed ? 1 : 0))) - 1
          unless val.between?(low, high)
            raise Refused.new('out_of_range', 'message integer exceeds its storage domain') if pending.nil?

            pending << 'out_of_range'
          end
        end
        return
      end
      if [10, 11].include?(kind)
        if shape.packing == 2
          qmin, qmax, qres = shape.quant
          delta = float32_round(qmax - qmin)
          ratio = float32_round(delta / qres)
          count = ratio.clamp(1.0, COMPRESSED_FLOAT_VALUES_MAX).ceil
          raise Refused.new('wire_damaged', 'compressed float index exceeds count') if reader.get(shape.bits) > count
        elsif kind == 10
          bits = reader.get(32)
          if (bits >> 23).allbits?(0xFF)
            raise Refused, 'non_finite_float' if pending.nil?

            pending << 'non_finite_float'
          end
        else
          bits = reader.get(64)
          if (bits >> 52).allbits?(0x7FF)
            raise Refused, 'non_finite_float' if pending.nil?

            pending << 'non_finite_float'
          end
        end
        return
      end
      if kind == 12
        len = reader.get(shape.maximum.bit_length)
        raise Refused.new('wire_damaged', 'text length exceeds announcement') if len > shape.maximum

        reader.align
        decode_wire_text(reader.raw(len), 'invalid message text')
        return
      end
      if kind == 13
        seen = SchemaTable.map_h
        loop do
          entry = msg_reference(reader, vocab)
          return if entry.nil?
          raise Refused.new('wire_damaged', 'nested message repeats a field') if seen[entry.field_id]

          seen[entry.field_id] = true
          msg_skip_value(reader, entry.shape, vocab, depth + 1, in_field: true, pending: pending)
        end
      end
      if [15, 30].include?(kind)
        entry = msg_reference(reader, vocab)
        if entry.nil?
          raise Refused.new('wire_damaged', 'a union field explicitly carries None') if kind == 15 && in_field

          return
        end
        if (kind == 30 && !entry.kind.zero?) || (kind == 15 && entry.kind.zero?)
          raise Refused.new('wire_damaged', 'message name has the wrong framing')
        end

        msg_skip_value(reader, entry.shape, vocab, depth + 1, in_field: false, pending: pending) if kind == 15
        return
      end
      if [14, 16].include?(kind)
        min_c = kind == 14 ? shape.minimum : 0
        count = min_c + reader.get((shape.maximum - min_c).bit_length)
        raise Refused.new('wire_damaged', 'invalid message array count') if count > shape.maximum || shape.inner.nil?

        reader.align if kind == 14 && shape.element == 6
        inner = shape.inner
        return if kind == 14 && (inner.kind.zero? || inner.kind == 32)

        plain_num = (inner.kind.between?(2, 9) || inner.kind.between?(18, 29)) && inner.packing.zero?
        if kind == 14 && ([1, 36].include?(inner.kind) || plain_num)
          width = if inner.kind == 1
                    1
                  else
                    (inner.kind == 36 ? 64 : msg_kind_bits(inner.kind))
                  end
          bits = count * width
          if bits > (reader.data.bytesize * 8) - reader.position
            raise Refused.new('wire_damaged',
                              'message array exceeds the remaining bits')
          end

          reader.position += bits
          return
        end
        if kind == 14 && inner.packing == 1 && inner.bits.zero?
          msg_skip_value(reader, inner, vocab, depth + 1, in_field: false, pending: pending)
          return
        end
        seen_keys = SchemaTable.map_h
        count.times do
          start = reader.position
          if kind == 16
            key = msg_reference(reader, vocab)
            if key.nil? || !key.kind.zero? || seen_keys[key.field_id]
              raise Refused.new('wire_damaged', 'invalid or repeated message key')
            end

            seen_keys[key.field_id] = true
          end
          msg_skip_value(reader, inner, vocab, depth + 1, in_field: false, pending: pending)
          raise Refused.new('wire_damaged', 'message element consumes no bits') if reader.position == start
        end
        return
      end
      if kind == 31
        reader.align
        len = reader.get(32)
        if len > ((reader.data.bytesize * 8) - reader.position) >> 3
          raise Refused.new('wire_damaged',
                            'escape exceeds message batch')
        end

        reader.position += len * 8
        return
      end
      raise Refused.new('wire_damaged', 'invalid message body kind')
    end

    def self.msg_scan_body(reader, vocab, depth, pending)
      raise Refused.new('wire_damaged', 'message nesting exceeds 128') if depth > MAX_DEPTH

      seen = SchemaTable.map_h
      loop do
        entry = msg_reference(reader, vocab)
        return if entry.nil?
        raise Refused.new('wire_damaged', 'message field repeated') if seen[entry.field_id]

        seen[entry.field_id] = true
        msg_skip_value(reader, entry.shape, vocab, depth, in_field: true, pending: pending)
      end
    end

    def self.encode_message_batch(root, values, vocab, keys, roots, measure, fast = nil)
      raise InvalidValue.new('', 'not an Array') unless values.is_a?(Array)
      raise InvalidValue.new('', 'batch_too_large') unless values.length.between?(1, 256)

      root_name = root.respond_to?(:descriptor) ? root.descriptor.name : ''
      if !vocab.announced || !roots[root_name] || !by_name(root_name).equal?(root)
        raise InvalidValue.new('', 'message root or vocabulary is invalid')
      end

      values.each do |v|
        raise InvalidValue.new('', 'batch contains another root') unless obj_class(v).equal?(root)

        validate_table_graph(v, root.descriptor, '', SchemaTable.map_h)
      end
      writer = MsgBitWriter.new(measure: measure)
      writer.put(2, 8)
      writer.put(values.length - 1, 8)
      values.each do |v|
        if fast
          fast.call(writer, v)
        else
          msg_write_body(writer, v, vocab, keys)
        end
      end
      writer.align
      writer
    end

    def self.save_message_batch(root, values, vocab, keys, roots, fast = nil)
      encode_message_batch(root, values, vocab, keys, roots, false, fast).data
    end

    def self.measure_message_batch(root, values, vocab, keys, roots, fast = nil)
      encode_message_batch(root, values, vocab, keys, roots, true, fast).position >> 3
    end

    def self.load_message_batch(root, vocab, data, own_version, keys, roots, capacity = 256, report = nil)
      root_name = root.respond_to?(:descriptor) ? root.descriptor.name : ''
      raise Refused, 'invalid_root' if !roots[root_name] || !by_name(root_name).equal?(root)

      raw = binary_input(data)
      raise Refused.new('wire_damaged', 'empty batch') if raw.empty?
      raise Refused, 'newer_form' if raw.getbyte(0) != 2
      raise Refused, 'no_vocabulary' unless vocab.announced
      raise Refused.new('wire_damaged', 'missing batch count') if raw.bytesize < 2

      count = raw.getbyte(1) + 1
      raise Refused, 'batch_too_large' if count > capacity

      scan = MsgBitReader.new(raw, 16)
      pending = SchemaTable.arr
      count.times { msg_scan_body(scan, vocab, 0, pending) }
      scan.finish
      raise Refused, pending[0] unless pending.empty?

      reader = MsgBitReader.new(raw, 16)
      stats = [0, 0, 0]
      values = Array.new(count) { msg_read_body(reader, root, vocab, own_version, keys, stats) }
      reader.finish
      unless report.nil?
        report.unknown = stats[0]
        report.widened = stats[1]
        report.retained = 0
        report.retain_lost = 0
      end
      values
    end
  end
end
