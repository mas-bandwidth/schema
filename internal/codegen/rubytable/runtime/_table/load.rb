# frozen_string_literal: true

module SchemaRuntimeUnit
  # Form-1 binary reader and wire check.
  module SchemaTable
    FORM = 1
    NODE_WIRE_ID = 0xFFFFFFFFFFFFFFFF
    BUILD_VERSION_WIRE_ID = 0xFFFFFFFFFFFFFFFE
    MESSAGE_VOCABULARY_WIRE_ID = 0xFFFFFFFFFFFFFFFD
    BYTES_WIRE_TYPE_ID = 0x2F2EC0474F1C4FE4
    STRING_WIRE_TYPE_ID = 0x704BE0D8FAAFFC58
    ID_BOUND = 4096
    MAX_DEPTH = 128
    COUNT_CAP = 0x7FFFFFFF
    BLOB_CAP = 0xFFFFFFFF

    FIXED_WIDTH = {
      1 => 1, 2 => 1, 3 => 2, 4 => 4, 5 => 8,
      6 => 1, 7 => 2, 8 => 4, 9 => 8,
      10 => 4, 11 => 8,
      18 => 16, 19 => 16,
      20 => 1, 21 => 2, 22 => 4, 23 => 8, 24 => 16,
      25 => 1, 26 => 2, 27 => 4, 28 => 8, 29 => 16,
      36 => 8
    }.freeze

    ARRAY_ELEMENT_KINDS = (
      (1..13).to_a + [15, 17] + (18..30).to_a
    ).to_h { |k| [k, true] }.freeze

    KEYED_ELEMENT_KINDS = (
      (1..11).to_a + [13] + (18..30).to_a
    ).to_h { |k| [k, true] }.freeze

    STRUCTURAL_KINDS = { 13 => true, 14 => true, 15 => true, 16 => true }.freeze
    LENGTH_KINDS = { 12 => true, 13 => true, 14 => true, 16 => true, 31 => true, 32 => true }.freeze

    SIGNED_WIDEN = [2, 3, 4, 5, 18].freeze
    UNSIGNED_WIDEN = [6, 7, 8, 9, 19].freeze

    BlobValue = Struct.new(:string, :value)
    BlobNode = Struct.new(:string, :body)

    # Bounded cursor over an ASCII-8BIT wire String.
    class Reader
      attr_reader :data, :ids
      attr_accessor :pos, :end_pos

      def initialize(data, pos, end_pos, ids)
        @data = data
        @pos = pos
        @end_pos = end_pos
        @ids = ids
      end

      def byte
        raise Refused.new('wire_damaged', 'a body ended before its framing') if @pos >= @end_pos

        val = @data.getbyte(@pos)
        @pos += 1
        val
      end

      def take(len)
        raise Refused.new('wire_damaged', 'a length runs past its body') if len.negative? || @pos + len > @end_pos

        slice = @data.byteslice(@pos, len)
        @pos += len
        slice
      end

      def varint
        val = 0
        shift = 0
        count = 0
        loop do
          raw = byte
          count += 1
          raise Refused.new('wire_damaged', 'a reference is longer than ten bytes') if count > 10
          raise Refused.new('wire_damaged', 'a reference crosses the 64th bit') if count == 10 && (raw & 0x7F) > 1

          val |= (raw & 0x7F) << shift
          if raw.nobits?(0x80)
            raise Refused.new('wire_damaged', 'a non-canonical reference') if count > 1 && raw.zero?

            return val
          end
          shift += 7
        end
      end

      def id_at(reference)
        if reference < 1 || reference > @ids.length
          raise Refused.new('wire_damaged',
                            'a reference is past the id table')
        end

        @ids[reference - 1]
      end
    end

    # Tracks first-use order of ID table entries during the pre-walk.
    class FirstUseIds
      def initialize(ids)
        @ids = ids
        @used = SchemaTable.map_h
        @order = SchemaTable.arr
      end

      def length
        @ids.length
      end

      def [](index)
        val = @ids[index]
        unless @used.key?(index)
          @used[index] = true
          @order << index
        end
        val
      end

      def finish
        raise Refused.new('wire_damaged', 'the id table contains an unused id') if @used.length != @ids.length

        @order.each_with_index do |pos, idx|
          raise Refused.new('wire_damaged', 'the id table is not in first-use order') if pos != idx
        end
      end
    end

    # Mutable state for one form-1 decode or wire-check walk.
    class LoadCtx
      attr_accessor :ids, :unknown, :widened, :missing, :pending, :blob_pending,
                    :current, :root, :nodes, :types, :saw_node, :depth,
                    :edges, :children, :blobs, :placeable, :variable, :nonfinite

      def initialize(ids)
        @ids = ids
        @unknown = 0
        @widened = 0
        @missing = SchemaTable.arr
        @pending = SchemaTable.arr
        @blob_pending = SchemaTable.arr
        @current = nil
        @root = nil
        @nodes = SchemaTable.arr
        @types = SchemaTable.arr
        @saw_node = false
        @depth = 0
        @edges = SchemaTable.arr
        @children = [@edges]
        @blobs = SchemaTable.arr
        @placeable = SchemaTable.map_h
        @variable = false
        @nonfinite = nil
      end
    end

    def self.widens?(wire, declared)
      s_w = SIGNED_WIDEN.index(wire)
      s_d = SIGNED_WIDEN.index(declared)
      return true if s_w && s_d && s_w < s_d

      u_w = UNSIGNED_WIDEN.index(wire)
      u_d = UNSIGNED_WIDEN.index(declared)
      return true if u_w && u_d && u_w < u_d

      wire == 10 && declared == 11
    end

    def self.read_widened_elements(sub, wire, element, count)
      if wire == 10
        raw = sub.take(4 * count)
        out = Array.new(count)
        count.times do |i|
          bits = raw.byteslice(i * 4, 4).unpack1('V')
          raise Refused.new('non_finite_float', element.name) if (bits >> 23).allbits?(0xFF)

          out[i] = [bits].pack('V').unpack1('e')
        end
        return out
      end

      width = FIXED_WIDTH[wire]
      signed = SIGNED_WIDEN.include?(wire)
      raw = sub.take(width * count)
      lo = element.lo
      hi = element.hi
      out = Array.new(count)
      count.times do |i|
        val = unpack_int_le(raw, i * width, width, signed)
        raise Refused.new('out_of_range', element.name) if val < lo || val > hi

        out[i] = val
      end
      out
    end

    def self.read_widened_scalar(reader, kind, field)
      val = read_widened_elements(reader, kind, field, 1)[0]
      raise Refused.new('out_of_range', field.name) if field.float_range && !float_in_range?(field, val)

      val
    end

    def self.load_table(cls, data, report = nil, retain = nil)
      raw = binary_input(data)
      ids = parse_id_table(raw)
      check_id_order(raw, ids)
      ctx = LoadCtx.new(ids)
      obj = cls.new
      ctx.root = obj
      reader = Reader.new(raw, 1, body_end_offset(raw), ids)
      begin
        read_table_body(reader, obj, ctx, true)
        if reader.pos != reader.end_pos
          raise Refused.new('wire_damaged', 'bytes between the root terminator and the id table')
        end
      rescue Refused => e
        if e.reason != 'wire_damaged'
          begin
            check_wire_file(raw, ids)
          rescue Refused => walked
            raise walked if walked.reason != e.reason
          end
        end
        raise
      end
      resolve_graph(ctx)
      raise Refused.new('missing_required', ctx.missing.join(', ')) unless ctx.missing.empty?

      retained = 0
      lost = 0
      unless retain.nil?
        retained, lost = capture_retain_file(cls, raw, retain, ids)
        retain.directory = ctx.nodes
        retain.loaded_root = obj
      end
      unless report.nil?
        report.unknown = ctx.unknown
        report.widened = ctx.widened
        report.retained = retained
        report.retain_lost = lost
      end
      obj
    end

    def self.body_end_offset(raw)
      count = unpack_int_le(raw, raw.bytesize - 8, 8, false)
      raw.bytesize - 8 - (8 * count)
    end

    def self.parse_id_table(raw)
      n = raw.bytesize
      raise Refused.new('wire_damaged', 'a file is shorter than its framing') if n < 10

      form = raw.getbyte(0)
      if form != FORM
        raise Refused.new('message_form_as_file', 'a message form is not a table file') if form == 2
        raise Refused.new('fixed_form_as_file', 'a fixed form is not this reader file') if form == 3

        raise Refused.new('newer_form', "form #{form} is not a form this build carries")
      end

      count = unpack_int_le(raw, n - 8, 8, false)
      raise Refused.new('wire_damaged', "an id table of #{count} entries is past the bound") if count > ID_BOUND

      start = n - 8 - (8 * count)
      raise Refused.new('wire_damaged', 'an id table leaves no room for a root body') if start < 2

      ids = Array.new(count)
      seen = SchemaTable.map_h
      count.times do |idx|
        tid = unpack_int_le(raw, start + (8 * idx), 8, false)
        raise Refused.new('wire_damaged', 'an id table carries one id twice') if seen[tid]

        seen[tid] = true
        ids[idx] = tid
      end
      ids
    end

    def self.read_nested_body(data, obj, ctx, map_key = nil)
      reader = Reader.new(data, 0, data.bytesize, ctx.ids)
      raise Refused.new('wire_damaged', 'a framed payload nested past the binary bound') if ctx.depth >= MAX_DEPTH

      ctx.depth += 1
      widened = read_table_body(reader, obj, ctx, false, map_key)
      ctx.depth -= 1
      raise Refused.new('wire_damaged', 'bytes left inside a body') if reader.pos != reader.end_pos

      widened
    end

    def self.read_table_body(reader, obj, ctx, root, map_key = nil)
      desc = obj_class(obj).descriptor
      previous = ctx.current
      ctx.current = obj
      seen = SchemaTable.map_h
      widened_key = false
      loop do
        reference = reader.varint
        break if reference.zero?

        raise Refused.new('wire_damaged', 'the node table is not the root body last field') if root && ctx.saw_node

        field_id = reader.id_at(reference)
        kind = reader.byte
        raise Refused.new('wire_damaged', 'a body carries one field twice') if seen[field_id]

        seen[field_id] = true
        field = desc.by_id[field_id]
        if field.nil?
          read_unknown_field(reader, field_id, kind, ctx, root)
          next
        end
        if kind == field.kind
          val = read_field_codec(reader, field, ctx)
        else
          unless widens?(kind, field.kind)
            raise Refused.new('kind_mismatch', "#{desc.name}.#{field.name} rides under kind #{kind}, not #{field.kind}")
          end

          val = read_widened_scalar(reader, kind, field)
          if field.equal?(map_key)
            widened_key = true
          else
            ctx.widened += 1
          end
        end
        field.set(obj, val)
      end
      desc.fields.each do |field|
        ctx.missing << field.name if field.required && field.guard_holds?(obj) && !seen[field.field_id]
      end
      ctx.current = previous
      widened_key
    end

    def self.read_unknown_field(reader, field_id, kind, ctx, root)
      if field_id == NODE_WIRE_ID
        raise Refused.new('wire_damaged', 'the node table is not in the root body') unless root
        raise Refused.new('wire_damaged', 'the node table rides twice') if ctx.saw_node
        raise Refused.new('wire_damaged', 'the node table is not a kind-12 payload') if kind != 12

        len = reader.varint
        read_node_table(reader.take(len), ctx)
        ctx.saw_node = true
        return
      end
      if [BUILD_VERSION_WIRE_ID, MESSAGE_VOCABULARY_WIRE_ID].include?(field_id)
        raise Refused.new('wire_damaged', 'a reserved announcement id in a file body')
      end

      skip_payload(reader, kind, ctx, ctx.depth)
      ctx.unknown += 1
    end

    def self.placeable_for_root(root_cls)
      return root_cls.instance_variable_get(:@_s_placeable) if root_cls.instance_variable_defined?(:@_s_placeable)

      out = SchemaTable.map_h
      visited = SchemaTable.map_h
      stack = [root_cls]
      until stack.empty?
        cls = stack.pop
        desc = cls.descriptor
        next if visited[desc.name]

        visited[desc.name] = true
        reached_fields(desc.fields).each do |field|
          if field.codec == 'bytes_blob'
            out[BYTES_WIRE_TYPE_ID] = true
          elsif field.codec == 'string_blob'
            out[STRING_WIRE_TYPE_ID] = true
          end
          next if field.target.empty?

          target = by_name(field.target)
          next if target.nil?

          out[target.descriptor.type_id] = true if field.pointer || field.codec == 'pointer'
          stack << target
        end
      end
      out.freeze
      root_cls.instance_variable_set(:@_s_placeable, out)
      out
    end

    def self.reached_fields(fields)
      out = SchemaTable.arr
      unions = SchemaTable.arr
      fields.each do |field|
        out << field
        unions << field.target if %w[union union_array].include?(field.codec)
      end
      seen = SchemaTable.map_h
      until unions.empty?
        name = unions.pop
        next if seen[name]

        seen[name] = true
        cls = union_by_name(name)
        next if cls.nil?

        cls.descriptor.arms.each do |arm|
          next if arm.field.nil?

          out << arm.field
          unions << arm.field.target if %w[union union_array].include?(arm.field.codec)
        end
      end
      out
    end

    def self.read_node_table(payload, ctx)
      root_cls = obj_class(ctx.root)
      ctx.placeable = placeable_for_root(root_cls)
      ctx.variable = root_cls.descriptor.variable
      reader = Reader.new(payload, 0, payload.bytesize, ctx.ids)
      count = reader.varint
      raise Refused.new('count_over_extent_cap', 'a node count above the int32 storage cap') if count > COUNT_CAP

      root_edges = ctx.edges
      nodes = SchemaTable.arr
      types = SchemaTable.arr
      count.times do
        type_ref = reader.varint
        raise Refused.new('wire_damaged', 'a node record has no type id') if type_ref.zero?

        type_id = reader.id_at(type_ref)
        len = reader.varint
        blob = [BYTES_WIRE_TYPE_ID, STRING_WIRE_TYPE_ID].include?(type_id)
        if blob && len > BLOB_CAP
          raise Refused.new('blob_over_size_cap',
                            'a byte buffer record is past the storage cap')
        end

        body = reader.take(len)
        if [NODE_WIRE_ID, BUILD_VERSION_WIRE_ID, MESSAGE_VOCABULARY_WIRE_ID].include?(type_id)
          raise Refused.new('wire_damaged', 'a reserved id in a record type position')
        end

        edges = SchemaTable.arr
        ctx.children << edges
        types << type_id
        if blob
          ctx.unknown += 1 if !ctx.placeable[type_id] && ctx.variable
          if type_id == STRING_WIRE_TYPE_ID
            txt = decode_wire_text(body, 'a string blob record that is not well-formed text')
            nodes << BlobValue.new(true, txt)
          else
            nodes << BlobValue.new(false, body.b)
          end
          ctx.blobs << true
          next
        end
        ctx.blobs << false
        cls = u(ctx.placeable[type_id] ? by_id(type_id) : nil)
        ctx.edges = edges
        saved_depth = ctx.depth
        ctx.depth = 0
        if cls.nil?
          skip_body(body, ctx, 0)
          nodes << nil
          ctx.unknown += 1 if ctx.variable
        else
          node = cls.new
          nodes << node
          sub = Reader.new(body, 0, body.bytesize, ctx.ids)
          read_table_body(sub, node, ctx, false)
          raise Refused.new('wire_damaged', 'bytes left inside a node record') if sub.pos != sub.end_pos
        end
        ctx.depth = saved_depth
        ctx.edges = root_edges
      end
      raise Refused.new('wire_damaged', 'bytes left inside the node table') if reader.pos != reader.end_pos

      ctx.nodes = nodes
      ctx.types = types
    end

    def self.check_pointer_graph(ctx)
      children = ctx.children
      n = children.length
      names = Array.new(n, 0)
      children.each do |out|
        out.each do |index|
          next if index.zero?
          raise Refused.new('wire_damaged', 'a pointer index past the node table') if index - 1 >= n

          names[index - 1] += 1
        end
      end
      ctx.blobs.each_with_index do |blob, k|
        raise Refused.new('blob_shared', 'one blob record named from two slots') if blob && names[k + 1] > 1
      end

      mark = Array.new(n, 0)
      stack = [[0, 0]]
      mark[0] = 1
      until stack.empty?
        top = stack[-1]
        out = children[top[0]]
        if top[1] == out.length
          mark[top[0]] = 2
          stack.pop
          next
        end
        index = out[top[1]]
        top[1] += 1
        next if index.zero?

        target = index - 1
        if mark[target] == 1
          raise Refused.new('wire_damaged',
                            'a pointer to a node whose descent is still open: a cycle')
        end
        next if mark[target] == 2

        mark[target] = 1
        stack << [target, 0]
      end
      (1...n).each do |k|
        raise Refused.new('wire_damaged', 'a node record no pointer reaches') if mark[k].zero?
      end
    end

    def self.resolve_graph(ctx)
      check_pointer_graph(ctx) if ctx.children.length > 1 || !ctx.edges.empty?
      ctx.pending.each do |owner, slot, index|
        if owner.instance_of?(Hash)
          key, field = slot
        elsif owner.instance_of?(Array)
          pos, field = slot
        else
          field = slot
        end
        val = nil
        unless index.zero?
          node = ctx.nodes[index - 2]
          if node.nil?
            type_id = ctx.types[index - 2]
            unless by_id(type_id).nil?
              raise Refused.new('wire_damaged', 'a pointer whose target is not the type its slot requires')
            end
          else
            target = by_name(field.target)
            if target.nil? || !obj_class(node).equal?(target)
              raise Refused.new('wire_damaged', 'a pointer whose target is not the type its slot requires')
            end

            val = node
          end
        end
        if owner.instance_of?(Hash)
          owner[key] = val
        elsif owner.instance_of?(Array)
          owner[pos] = val
        else
          field.set(owner, val)
        end
      end

      ctx.blob_pending.each do |owner, slot, index|
        if owner.instance_of?(Hash)
          key, field = slot
        else
          field = slot
        end
        val = nil
        unless index.zero?
          buffer = ctx.nodes[index - 2]
          if !buffer.is_a?(BlobValue)
            unless by_id(ctx.types[index - 2]).nil?
              raise Refused.new('wire_damaged', 'a byte buffer slot names a table record')
            end
          elsif buffer.string != (field.codec == 'string_blob')
            raise Refused.new('wire_damaged', 'a byte buffer slot names the other buffer kind')
          else
            val = buffer.value
          end
        end
        if owner.instance_of?(Hash)
          owner[key] = val
        else
          field.set(owner, val)
        end
      end
    end

    # Codec read dispatch
    def self.read_field_codec(reader, fd, ctx)
      case fd.codec
      when 'bool'
        read_bool_element(reader)
      when 'int'
        read_int_element(reader, fd)
      when 'float32', 'float64'
        read_float_element(reader, fd)
      when 'string'
        read_string_element(reader, fd)
      when 'bytes'
        read_bytes_field(reader, fd)
      when 'enum'
        read_enum_element(reader, fd, ctx)
      when 'table'
        read_table_element(reader, fd, ctx)
      when 'text_optional'
        rec = read_table_element(reader, fd, ctx)
        obj_class(rec).descriptor.fields[0].get(rec)
      when 'pointer'
        idx = reader.varint
        ctx.pending << [ctx.current, fd, idx]
        ctx.edges << idx
        nil
      when 'pointer_array'
        read_pointer_array_field(reader, fd, ctx)
      when 'string_blob', 'bytes_blob'
        idx = reader.varint
        ctx.blob_pending << [ctx.current, fd, idx]
        ctx.edges << idx
        nil
      when 'optional'
        read_optional_field(reader, fd, ctx)
      when 'array'
        read_array_field(reader, fd, ctx)
      when 'keyed'
        read_keyed_field(reader, fd, ctx)
      when 'map'
        read_map_field(reader, fd, ctx)
      when 'union'
        un_cls = union_by_name(fd.target)
        raise InvalidValue.new(fd.target, "the union #{fd.target} is not registered") if un_cls.nil?

        read_union_payload(reader, un_cls, ctx)
      when 'union_array'
        read_union_array_field(reader, fd, ctx)
      else
        raise Refused.new('unsupported_kind', "unsupported codec #{fd.codec}")
      end
    end

    def self.read_element_codec(reader, fd, ctx)
      case fd.codec
      when 'bool'
        read_bool_element(reader)
      when 'int'
        read_int_element(reader, fd)
      when 'float32', 'float64'
        read_float_element(reader, fd)
      when 'string'
        read_string_element(reader, fd)
      when 'enum'
        read_enum_element(reader, fd, ctx)
      when 'table'
        read_table_element(reader, fd, ctx)
      when 'text_optional'
        rec = read_table_element(reader, fd, ctx)
        obj_class(rec).descriptor.fields[0].get(rec)
      else
        raise Refused.new('unsupported_kind', "unsupported element codec #{fd.codec}")
      end
    end

    def self.read_bool_element(reader)
      raw = reader.byte
      raise Refused.new('wire_damaged', 'a bool payload is not 0 or 1') if raw > 1

      raw == 1
    end

    def self.read_int_element(reader, fd)
      width = FIXED_WIDTH[fd.kind]
      raise Refused.new('kind_mismatch', "kind #{fd.kind} is not an integer kind") if width.nil?

      raw = reader.take(width)
      val = unpack_int_le(raw, 0, width, fd.lo.negative?)
      raise Refused.new('out_of_range', "#{val} is outside [#{fd.lo}, #{fd.hi}]") if val < fd.lo || val > fd.hi

      val
    end

    def self.read_float_element(reader, fd)
      if fd.kind == 10
        raw = reader.take(4)
        bits = raw.unpack1('V')
        raise Refused.new('non_finite_float', "#{fd.name} is not a finite float") if (bits >> 23).allbits?(0xFF)

        val = raw.unpack1('e')
      else
        raw = reader.take(8)
        bits = raw.unpack1('Q<')
        raise Refused.new('non_finite_float', "#{fd.name} is not a finite float") if (bits >> 52).allbits?(0x7FF)

        val = raw.unpack1('E')
      end
      if fd.float_range
        if val < fd.fmin || val > fd.fmax
          raise Refused.new('out_of_range',
                            "#{val} is outside [#{fd.fmin}, #{fd.fmax}]")
        end
        return 0.0 if val.zero?
      end
      val
    end

    def self.decode_wire_text(raw, why)
      raise Refused.new('wire_damaged', why) if raw.include?("\x00".b)

      str = raw.dup.force_encoding(Encoding::UTF_8)
      raise Refused.new('wire_damaged', why) unless str.valid_encoding?

      str
    end

    def self.read_string_element(reader, fd)
      len = reader.varint
      raw = reader.take(len)
      str = decode_wire_text(raw, 'ill-formed text')
      if fd.size.positive? && len > fd.size
        raise Refused.new('length_over_bound',
                          "length #{len} is past its bound #{fd.size}")
      end

      str
    end

    def self.read_bytes_field(reader, fd)
      len = reader.varint
      body = reader.take(len)
      sub = Reader.new(body, 0, body.bytesize, reader.ids)
      raise Refused.new('kind_mismatch', 'a byte buffer element kind is not u8') if sub.byte != 6

      count = sub.varint
      if fd.size.positive? && count > fd.size
        raise Refused.new('length_over_bound',
                          "length #{count} is past its bound #{fd.size}")
      end

      out = sub.take(count).b
      raise Refused.new('wire_damaged', 'bytes left inside a byte buffer') if sub.pos != sub.end_pos

      out
    end

    def self.read_enum_element(reader, fd, ctx)
      ref = reader.varint
      return 0 if ref.zero?

      ordinal = fd.value_of[reader.id_at(ref)]
      if ordinal.nil?
        ctx.unknown += 1
        return 0
      end
      return fd.default_value if fd.deprecated[ordinal]

      ordinal
    end

    def self.read_table_element(reader, fd, ctx)
      len = reader.varint
      payload = reader.take(len)
      cls = by_name(fd.target)
      raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if cls.nil?

      obj = cls.new
      read_nested_body(payload, obj, ctx)
      obj
    end

    def self.read_pointer_array_field(reader, fd, ctx)
      len = reader.varint
      payload = reader.take(len)
      raise Refused.new('wire_damaged', 'an array body too short for its header') if len < 2

      sub = Reader.new(payload, 0, len, reader.ids)
      ek = sub.byte
      count = sub.varint
      raise Refused.new('wire_damaged', 'an array element kind outside the closed list') unless ARRAY_ELEMENT_KINDS[ek]
      raise Refused.new('kind_mismatch', "#{fd.name}: array element kind #{ek} is not 17") if ek != 17
      if count > COUNT_CAP
        raise Refused.new('count_over_extent_cap',
                          "#{fd.name}: count #{count} exceeds int32 storage cap")
      end
      if fd.array_mode != 'list' && count > fd.bound
        raise Refused.new('count_over_bound',
                          "#{fd.name}: count #{count} exceeds bound #{fd.bound}")
      end
      if fd.array_mode == 'counted' && count < fd.min_count
        raise Refused.new('count_over_bound', "#{fd.name}: count #{count} is below minimum #{fd.min_count}")
      end
      raise Refused.new('wire_damaged', 'a count the array body cannot cover') if count > sub.end_pos - sub.pos

      alloc_len = fd.array_mode == 'fixed' ? fd.bound : count
      out = Array.new(alloc_len)
      count.times do |slot|
        idx = sub.varint
        ctx.pending << [out, [slot, fd], idx]
        ctx.edges << idx
      end
      raise Refused.new('wire_damaged', 'bytes left inside an array after its elements') if sub.pos != sub.end_pos

      out
    end

    def self.read_optional_field(reader, fd, ctx)
      return read_optional_array(reader, fd, ctx) if fd.inner == 'array'

      inner_fd = FieldDesc.new(
        name: fd.name,
        field_id: fd.field_id,
        kind: fd.kind,
        codec: fd.inner,
        lo: fd.lo,
        hi: fd.hi,
        f32: fd.f32,
        float_range: fd.float_range,
        fmin: fd.fmin,
        fmax: fd.fmax,
        target: fd.target,
        size: fd.size,
        variant_ids: fd.variant_ids,
        unbounded: fd.unbounded,
        map_entry: fd.map_entry,
        bound: fd.bound,
        ordinal: fd.ordinal,
        deprecated: fd.deprecated,
        default: fd.default_value
      )
      read_field_codec(reader, inner_fd, ctx)
    end

    def self.read_optional_array(reader, fd, ctx)
      elem = fd.elem
      raise InvalidValue.new(fd.name, 'an optional array carries no element descriptor') if elem.nil?

      body = reader.take(reader.varint)
      raise Refused.new('wire_damaged', 'an array body is too short for its header') if body.bytesize < 2

      sub = Reader.new(body, 0, body.bytesize, ctx.ids)
      elem_kind = sub.byte
      unless ARRAY_ELEMENT_KINDS[elem_kind]
        raise Refused.new('wire_damaged',
                          "element kind #{elem_kind} is not in the closed set")
      end

      widened = elem_kind != elem.kind
      if widened && !widens?(elem_kind, elem.kind)
        raise Refused.new('kind_mismatch', "#{fd.name} rides element kind #{elem_kind}, not #{elem.kind}")
      end

      ctx.widened += 1 if widened
      count = sub.varint
      if count > fd.bound || (fd.array_kind == 2 && count < fd.array_min)
        raise Refused.new('count_over_bound', "#{count} elements is outside [#{fd.array_min}, #{fd.bound}]")
      end
      raise Refused.new('wire_damaged', 'a count the array body cannot cover') if count > sub.end_pos - sub.pos

      values = Array.new(count) do
        widened ? read_widened_scalar(sub, elem_kind, elem) : read_field_codec(sub, elem, ctx)
      end
      raise Refused.new('wire_damaged', 'bytes left inside an array body') if sub.pos != sub.end_pos

      if fd.array_kind == 1
        values << (elem.codec == 'table' ? new_by_name(elem.target) : elem.default_value) while values.length < fd.bound
      end
      values
    end

    def self.read_array_bulk(sub, element, count)
      if element.codec == 'bool'
        raw = sub.take(count)
        out = Array.new(count)
        count.times do |i|
          b = raw.getbyte(i)
          raise Refused.new('wire_damaged', 'a bool payload is not 0 or 1') if b > 1

          out[i] = (b == 1)
        end
        return out
      end
      return nil unless element.codec == 'int'

      width = FIXED_WIDTH[element.kind]
      return nil if width.nil? || width > 8

      directive = case width
                  when 1 then element.lo.negative? ? 'c*' : 'C*'
                  when 2 then element.lo.negative? ? 's<*' : 'S<*'
                  when 4 then element.lo.negative? ? 'l<*' : 'L<*'
                  when 8 then element.lo.negative? ? 'q<*' : 'Q<*'
                  end
      raw = sub.take(width * count)
      vals = raw.unpack(directive)
      lo = element.lo
      hi = element.hi
      vals.each do |v|
        raise Refused.new('out_of_range', "#{v} is outside [#{lo}, #{hi}]") if v < lo || v > hi
      end
      vals
    end

    def self.read_array_field(reader, fd, ctx)
      len = reader.varint
      payload = reader.take(len)
      raise Refused.new('wire_damaged', 'an array body too short for its header') if len < 2

      sub = Reader.new(payload, 0, len, ctx.ids)
      ek = sub.byte
      raise Refused.new('wire_damaged', 'an array element kind outside the closed list') unless ARRAY_ELEMENT_KINDS[ek]

      widened = false
      if ek != fd.elem_kind
        unless widens?(ek, fd.elem_kind)
          raise Refused.new('kind_mismatch', "#{fd.name} rides as an array of kind #{ek}, not #{fd.elem_kind}")
        end

        widened = true
      end
      count = sub.varint
      if count > COUNT_CAP
        raise Refused.new('count_over_extent_cap',
                          "an array count of #{count} is past the int32 storage cap")
      end
      if fd.array_kind == 1 && count > fd.bound
        raise Refused.new('count_over_bound',
                          "#{fd.name} carries #{count} elements where at most #{fd.bound} are declared")
      end
      if fd.array_kind == 2 && (count < fd.array_min || (fd.bound.positive? && count > fd.bound))
        raise Refused.new('count_over_bound',
                          "#{fd.name} carries #{count} elements outside [#{fd.array_min}, #{fd.bound}]")
      end
      raise Refused.new('wire_damaged', 'a count the array body cannot cover') if count > len - sub.pos

      element = fd.element
      raise Refused.new('unsupported_kind', 'an array carries no element descriptor') if element.nil?

      if widened
        items = read_widened_elements(sub, ek, element, count)
        ctx.widened += 1
      else
        bulk = read_array_bulk(sub, element, count)
        items = bulk || Array.new(count) { read_element_codec(sub, element, ctx) }
      end
      raise Refused.new('wire_damaged', 'bytes left inside an array L after its elements') if sub.pos != len

      if fd.text_total.positive?
        total = 0
        items.each { |elem| total += elem.bytesize unless elem.nil? }
        raise Refused.new('length_over_bound', "#{fd.name} exceeds its text total") if total > fd.text_total
      end
      if fd.array_kind == 1 && count < fd.bound
        (fd.bound - count).times do
          items << (element.codec == 'table' ? new_by_name(element.target) : element.default_value)
        end
      end
      items
    end

    def self.read_keyed_field(reader, fd, ctx)
      elem = fd.elem
      raise InvalidValue.new(fd.name, 'a keyed field without an element descriptor') if elem.nil?

      len = reader.varint
      payload = reader.take(len)
      raise Refused.new('wire_damaged', 'a keyed body too short for its header') if len < 2

      body = Reader.new(payload, 0, len, ctx.ids)
      elem_kind = body.byte
      count = body.varint
      unless KEYED_ELEMENT_KINDS[elem_kind]
        raise Refused.new('wire_damaged',
                          "a keyed element kind #{elem_kind} outside the closed list")
      end

      if elem_kind != elem.kind
        unless widens?(elem_kind, elem.kind)
          raise Refused.new('kind_mismatch', "#{fd.name} carries element kind #{elem_kind}, not #{elem.kind}")
        end

        ctx.widened += 1
      end

      current = fd.get(ctx.current)
      out = if fd.keyed_list
              current
            else
              obj_class(current).new(current.key_max) do
                elem.codec == 'table' ? new_by_name(elem.target) : elem.default_value
              end
            end
      values = fd.keyed_list ? out : out._s_values
      target = u(elem.codec == 'table' ? by_name(elem.target) : nil)
      seen = SchemaTable.map_h
      count.times do
        ref = body.varint
        key_id = body.id_at(ref)
        raise Refused.new('wire_damaged', 'the same keyed slot twice in one keyed body') if seen[ref]

        seen[ref] = true
        size = body.varint
        element_bytes = body.take(size)
        slot = fd.keys.index(key_id)
        if slot.nil?
          skip_content(element_bytes, elem_kind, ctx, ctx.depth)
          ctx.unknown += 1
          next
        end
        if target
          obj = target.new
          read_nested_body(element_bytes, obj, ctx)
          values[slot] = obj
          next
        end
        sub = Reader.new(element_bytes, 0, size, ctx.ids)
        values[slot] = if elem_kind == elem.kind
                         read_field_codec(sub, elem, ctx)
                       else
                         read_widened_scalar(sub, elem_kind, elem)
                       end
        raise Refused.new('wire_damaged', 'bytes left inside a keyed slot after its element') if sub.pos != sub.end_pos
      end
      raise Refused.new('wire_damaged', 'bytes left inside a keyed array L after its slots') if body.pos != body.end_pos

      out
    end

    def self.compare_map_keys(key_field, lhs, rhs)
      if key_field.codec == 'string'
        lhs.b <=> rhs.b
      else
        lhs <=> rhs
      end
    end

    def self.sorted_map_keys(fd, mapping)
      entry_cls = by_name(fd.target)
      raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if entry_cls.nil?

      key_field = entry_cls.descriptor.fields[0]
      if key_field.codec == 'string'
        mapping.keys.sort_by(&:b)
      else
        mapping.keys.sort
      end
    end

    def self.read_map_field(reader, fd, ctx)
      entry_cls = by_name(fd.target)
      raise InvalidValue.new(fd.name, "the table #{fd.target} is not registered") if entry_cls.nil?

      key_field = entry_cls.descriptor.fields[0]
      value_field = entry_cls.descriptor.fields[1]
      len = reader.varint
      payload = reader.take(len)
      raise Refused.new('wire_damaged', 'a map body too short for its header') if len < 2

      sub = Reader.new(payload, 0, len, ctx.ids)
      elem_kind = sub.byte
      if elem_kind != 13
        raise Refused.new('kind_mismatch',
                          "#{fd.name} holds map entries under kind #{elem_kind}, not 13")
      end

      count = sub.varint
      if count > COUNT_CAP
        raise Refused.new('count_over_extent_cap',
                          "#{fd.name} holds #{count} entries, past the int32 storage cap")
      end
      raise Refused.new('wire_damaged', 'a count the map body cannot cover') if count > sub.end_pos - sub.pos

      out = SchemaTable.map_h
      last = nil
      landed = false
      widened = false
      count.times do
        n = sub.varint
        body = sub.take(n)
        entry = entry_cls.new
        before = ctx.pending.length
        blobs_before = ctx.blob_pending.length
        if read_nested_body(body, entry, ctx, key_field) && !widened
          ctx.widened += 1
          widened = true
        end
        key = key_field.get(entry)
        if landed && compare_map_keys(key_field, last, key) >= 0
          raise Refused.new('wire_damaged', "#{fd.name} is not in ascending key order")
        end

        last = key
        landed = true
        if value_field.codec == 'pointer'
          (before...ctx.pending.length).each do |i|
            owner, slot, index = ctx.pending[i]
            ctx.pending[i] = [out, [key, slot], index] if owner.equal?(entry) && slot.name == value_field.name
          end
        end
        if %w[string_blob bytes_blob].include?(value_field.codec)
          (blobs_before...ctx.blob_pending.length).each do |i|
            owner, slot, index = ctx.blob_pending[i]
            ctx.blob_pending[i] = [out, [key, slot], index] if owner.equal?(entry) && slot.name == value_field.name
          end
        end
        out[key] = value_field.get(entry)
      end
      raise Refused.new('wire_damaged', "#{fd.name} has bytes left inside its map body") if sub.pos != sub.end_pos

      out
    end

    def self.read_union_payload(reader, un_cls, ctx)
      reference = reader.varint
      return un_cls.new if reference.zero?

      arm_id = reader.id_at(reference)
      arm = un_cls.descriptor.by_id[arm_id]
      kind = reader.byte
      framed = reader.pos
      len = reader.varint
      start = reader.pos
      payload = reader.take(len)
      if arm.nil?
        depth = ctx.depth
        if STRUCTURAL_KINDS[kind]
          raise Refused.new('wire_damaged', 'a union arm nested past the binary bound') if depth >= MAX_DEPTH

          depth += 1
        end
        skip_content(payload, kind, ctx, depth)
        ctx.unknown += 1
        return un_cls.new
      end
      if kind != arm.kind
        if arm.field.nil? || !widens?(kind, arm.kind)
          raise Refused.new('kind_mismatch',
                            "#{un_cls.descriptor.name}.#{arm.name} rides under kind #{kind}, not #{arm.kind}")
        end

        sub = Reader.new(reader.data, start, reader.pos, reader.ids)
        loaded = read_widened_scalar(sub, kind, arm.field)
        raise Refused.new('wire_damaged', 'bytes left inside an arm L after its payload') if sub.pos != sub.end_pos

        val = un_cls.new
        val.type = arm.tag
        ivar_set(val, arm.ivar, loaded)
        ctx.widened += 1
        return val
      end
      val = un_cls.new
      val.type = arm.tag
      if arm.field.nil?
        raise Refused.new('wire_damaged', 'kind 32 with a nonzero L') unless len.zero?

        return val
      end
      previous = ctx.current
      ctx.current = val
      loaded = u(nil)
      begin
        sub_start = LENGTH_KINDS[arm.field.kind] ? framed : start
        sub = Reader.new(reader.data, sub_start, reader.pos, ctx.ids)
        loaded = if arm.field.codec == 'union'
                   inner_cls = union_by_name(arm.field.target)
                   if inner_cls.nil?
                     raise InvalidValue.new(arm.field.target,
                                            "the union #{arm.field.target} is not registered")
                   end

                   read_union_payload(sub, inner_cls, ctx)
                 else
                   read_field_codec(sub, arm.field, ctx)
                 end
        raise Refused.new('wire_damaged', 'bytes left inside an arm L after its payload') if sub.pos != sub.end_pos
      ensure
        ctx.current = previous
      end
      ivar_set(val, arm.ivar, loaded)
      val
    end

    def self.read_union_array_field(reader, fd, ctx)
      len = reader.varint
      payload = reader.take(len)
      raise Refused.new('wire_damaged', 'an array body too short for its header') if len < 2

      sub = Reader.new(payload, 0, len, ctx.ids)
      element_kind = sub.byte
      unless ARRAY_ELEMENT_KINDS[element_kind]
        raise Refused.new('wire_damaged',
                          'an array element kind outside the closed list')
      end
      if element_kind != 15
        raise Refused.new('kind_mismatch',
                          "#{fd.name} carries element kind #{element_kind}, not 15")
      end

      count = sub.varint
      if fd.array_kind == 3
        if count > COUNT_CAP
          raise Refused.new('count_over_extent_cap',
                            'a collection count above the int32 storage cap')
        end
      elsif count > fd.bound || (fd.array_kind == 2 && count < fd.array_min)
        raise Refused.new('count_over_bound', "#{fd.name} counts #{count}, outside its bound")
      end
      raise Refused.new('wire_damaged', 'a count the array body cannot cover') if count > sub.end_pos - sub.pos

      un_cls = union_by_name(fd.target)
      raise InvalidValue.new(fd.target, "the union #{fd.target} is not registered") if un_cls.nil?

      fixed = fd.array_kind == 1
      values = fixed ? Array.new(fd.bound) { un_cls.new } : SchemaTable.arr
      count.times do |idx|
        elem = read_union_payload(sub, un_cls, ctx)
        if fixed
          values[idx] = elem
        else
          values << elem
        end
      end
      raise Refused.new('wire_damaged', 'bytes left inside an array of unions') if sub.pos != sub.end_pos

      values
    end

    # Declaration-free wire check and skip walk (internal/tablewire/wirecheck.go)
    def self.check_scalar_wire(raw, kind, ctx)
      case kind
      when 1
        raise Refused.new('wire_damaged', 'a bool byte other than 0 or 1') if raw.getbyte(0) > 1
      when 10
        bits = raw.unpack1('V')
        record_nonfinite(ctx) if (bits >> 23).allbits?(0xFF)
      when 11
        bits = raw.unpack1('Q<')
        record_nonfinite(ctx) if (bits >> 52).allbits?(0x7FF)
      end
    end

    def self.record_nonfinite(ctx)
      raise Refused.new('non_finite_float', 'a float the wire carries') if ctx.nonfinite.nil?

      ctx.nonfinite << true
    end

    def self.check_wire_file(raw, ids)
      ctx = LoadCtx.new(ids)
      ctx.nonfinite = SchemaTable.arr
      reader = Reader.new(raw, 1, body_end_offset(raw), ids)
      seen = SchemaTable.map_h
      saw_node = false
      loop do
        reference = reader.varint
        break if reference.zero?
        raise Refused.new('wire_damaged', 'the node table is not the root body last field') if saw_node

        field_id = reader.id_at(reference)
        raise Refused.new('wire_damaged', 'the same field id twice in one body') if seen[field_id]

        seen[field_id] = true
        kind = reader.byte
        if [BUILD_VERSION_WIRE_ID, MESSAGE_VOCABULARY_WIRE_ID].include?(field_id)
          raise Refused.new('wire_damaged', 'a reserved announcement id out of place')
        end

        if field_id == NODE_WIRE_ID
          raise Refused.new('wire_damaged', 'the node table under a kind other than 12') if kind != 12

          check_node_table_wire(reader.take(reader.varint), ctx)
          saw_node = true
          next
        end
        skip_payload(reader, kind, ctx, 0)
      end
      return unless reader.pos != reader.end_pos

      raise Refused.new('wire_damaged', 'bytes between the root terminator and the id table')
    end

    def self.check_id_order(raw, ids, announcement: false)
      tracked = FirstUseIds.new(ids)
      begin
        walk_id_order(raw, tracked, announcement)
      rescue Refused
        return
      end
      tracked.finish
    end

    def self.walk_id_order(raw, tracked, announcement)
      ctx = LoadCtx.new(tracked)
      ctx.nonfinite = SchemaTable.arr
      reader = Reader.new(raw, 1, body_end_offset(raw), tracked)
      seen = SchemaTable.map_h
      nodes = nil
      loop do
        reference = reader.varint
        break if reference.zero?

        field_id = reader.id_at(reference)
        raise Refused.new('wire_damaged', 'the same field id twice in one body') if seen[field_id]

        seen[field_id] = true
        kind = reader.byte
        if [BUILD_VERSION_WIRE_ID, MESSAGE_VOCABULARY_WIRE_ID].include?(field_id) && !announcement
          raise Refused.new('wire_damaged', 'a reserved announcement id out of place')
        end

        if field_id == NODE_WIRE_ID
          raise Refused.new('wire_damaged', 'the node table has the wrong kind') if kind != 12

          nodes = reader.take(reader.varint)
        else
          skip_payload(reader, kind, ctx, 0)
        end
      end
      if reader.pos != reader.end_pos
        raise Refused.new('wire_damaged', 'bytes between the root terminator and the id table')
      end

      check_node_table_wire(nodes, ctx) unless nodes.nil?
    end

    def self.check_node_table_wire(data, ctx)
      reader = Reader.new(data, 0, data.bytesize, ctx.ids)
      count = reader.varint
      raise Refused.new('count_over_extent_cap', 'a node count above the int32 storage cap') if count > COUNT_CAP

      count.times do
        type_id = reader.id_at(reader.varint)
        body = reader.take(reader.varint)
        if type_id == STRING_WIRE_TYPE_ID
          decode_wire_text(body, 'a string blob record that is not well-formed text')
          next
        end
        next if type_id == BYTES_WIRE_TYPE_ID
        if [NODE_WIRE_ID, BUILD_VERSION_WIRE_ID, MESSAGE_VOCABULARY_WIRE_ID].include?(type_id)
          raise Refused.new('wire_damaged', 'a reserved id in a record type position')
        end

        skip_body(body, ctx, 0)
      end
      raise Refused.new('wire_damaged', 'bytes left inside the node table') if reader.pos != reader.end_pos
    end

    def self.skip_payload(reader, kind, ctx, depth)
      width = FIXED_WIDTH[kind]
      unless width.nil?
        raise Refused.new('wire_damaged', 'a fixed-width payload cut short') if width > reader.end_pos - reader.pos

        check_scalar_wire(reader.take(width), kind, ctx)
        return
      end
      case kind
      when 17
        ctx.edges << reader.varint
      when 30
        ref = reader.varint
        reader.id_at(ref) if ref.positive?
      when 15
        skip_union(reader, ctx, depth)
      when 12, 13, 14, 16, 31, 32
        len = reader.varint
        skip_content(reader.take(len), kind, ctx, depth)
      else
        raise Refused.new('wire_damaged', "kind #{kind} is a kind no reader can skip")
      end
    end

    def self.skip_content(data, kind, ctx, depth)
      width = FIXED_WIDTH[kind]
      unless width.nil?
        raise Refused.new('wire_damaged', 'an L that is not its kind width') if data.bytesize != width

        check_scalar_wire(data, kind, ctx)
        return
      end
      case kind
      when 12
        decode_wire_text(data, 'ill-formed text')
      when 31
        nil
      when 32
        raise Refused.new('wire_damaged', 'kind 32 with a nonzero L') unless data.bytesize.zero?
      when 13, 14, 16
        raise Refused.new('wire_damaged', 'a framed payload nested past the binary bound') if depth >= MAX_DEPTH

        if kind == 13
          skip_body(data, ctx, depth + 1)
        elsif kind == 14
          skip_array(data, ctx, depth + 1)
        else
          skip_keyed(data, ctx, depth + 1)
        end
      when 15, 17, 30
        reader = Reader.new(data, 0, data.bytesize, ctx.ids)
        skip_payload(reader, kind, ctx, depth)
        raise Refused.new('wire_damaged', 'bytes left inside an arm after its payload') if reader.pos != reader.end_pos
      else
        raise Refused.new('wire_damaged', "kind #{kind} is a kind no reader can skip")
      end
    end

    def self.skip_body(data, ctx, depth)
      reader = Reader.new(data, 0, data.bytesize, ctx.ids)
      seen = SchemaTable.map_h
      loop do
        reference = reader.varint
        if reference.zero?
          if reader.pos != reader.end_pos
            raise Refused.new('wire_damaged',
                              'bytes after a body terminator inside its L')
          end

          return
        end
        field_id = reader.id_at(reference)
        raise Refused.new('wire_damaged', 'the same field id twice in one body') if seen[field_id]

        seen[field_id] = true
        kind = reader.byte
        if [BUILD_VERSION_WIRE_ID, MESSAGE_VOCABULARY_WIRE_ID].include?(field_id)
          raise Refused.new('wire_damaged', 'a reserved announcement id out of place')
        end
        raise Refused.new('wire_damaged', 'the node table reserved id in a nested body') if field_id == NODE_WIRE_ID

        skip_payload(reader, kind, ctx, depth)
      end
    end

    def self.parse_array_header(data, ctx)
      raise Refused.new('wire_damaged', 'an array, keyed or map body too short for its header') if data.bytesize < 2

      reader = Reader.new(data, 1, data.bytesize, ctx.ids)
      count = reader.varint
      raise Refused.new('count_over_extent_cap', 'a collection count above the int32 storage cap') if count > COUNT_CAP

      [data.getbyte(0), count, reader]
    end

    def self.skip_array(data, ctx, depth)
      element, count, reader = parse_array_header(data, ctx)
      unless ARRAY_ELEMENT_KINDS[element]
        raise Refused.new('wire_damaged',
                          'a walked array element kind outside the closed list')
      end

      width = FIXED_WIDTH[element]
      if ctx.ids.is_a?(FirstUseIds) && !width.nil?
        reader.take(count * width)
        if reader.pos != reader.end_pos
          raise Refused.new('wire_damaged',
                            'bytes left inside an array L after its elements')
        end

        return
      end
      count.times { skip_payload(reader, element, ctx, depth) }
      return unless reader.pos != reader.end_pos

      raise Refused.new('wire_damaged',
                        'bytes left inside an array L after its elements')
    end

    def self.skip_keyed(data, ctx, depth)
      element, count, reader = parse_array_header(data, ctx)
      unless KEYED_ELEMENT_KINDS[element]
        raise Refused.new('wire_damaged',
                          'a keyed element kind outside the closed list')
      end

      seen = SchemaTable.map_h
      count.times do
        key = reader.varint
        if key.zero? || key > ctx.ids.length
          raise Refused.new('wire_damaged',
                            'a keyed slot whose key reference names no id')
        end

        reader.id_at(key)
        raise Refused.new('wire_damaged', 'the same keyed slot twice in one keyed body') if seen[key]

        seen[key] = true
        len = reader.varint
        skip_content(reader.take(len), element, ctx, depth)
      end
      return unless reader.pos != reader.end_pos

      raise Refused.new('wire_damaged',
                        'bytes left inside a keyed array L after its slots')
    end

    def self.skip_union(reader, ctx, depth)
      reference = reader.varint
      return if reference.zero?
      raise Refused.new('wire_damaged', 'an arm reference past the id table') if reference > ctx.ids.length

      reader.id_at(reference)
      kind = reader.byte
      len = reader.varint
      payload = reader.take(len)
      if STRUCTURAL_KINDS[kind]
        raise Refused.new('wire_damaged', 'a union arm nested past the binary bound') if depth >= MAX_DEPTH

        depth += 1
      end
      skip_content(payload, kind, ctx, depth)
    end
  end
end
