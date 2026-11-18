# frozen_string_literal: true

module SchemaRuntimeUnit
  # Hand-editable S-expression text document and transactional editor.
  module SchemaTable
    TEXT_EDIT_STEP_RE = %r{\G(?:\.?([A-Za-z_][A-Za-z_0-9/-]*)|\[([0-9]+)\])}

    # Immutable resolved hand-text document.
    class TextDocument
      attr_reader :source, :root, :value, :limits, :comments

      def initialize(cls, source, root, value, limits, spans)
        @cls = cls
        @source = source.b.freeze
        @root = root
        @value = value
        @limits = limits
        @comments = SchemaTable.attach_text_comments(@source, root, cls, spans)
      end

      def write
        @source
      end

      def edit(edits, replacement = nil)
        edits = [[edits, replacement]] unless replacement.nil?
        replacements = SchemaTable.arr
        spans = SchemaTable.arr
        original = SchemaTable.to_json(@value)
        edits.each do |path, repl_in|
          repl = repl_in.is_a?(String) ? repl_in.b : raise(TextError.new('text_edit_conflict', 0))
          parts = SchemaTable.parse_edit_path(path)
          cur = SchemaTable.locate_text_node(@root, @cls, parts)
          raise TextError.new('text_edit_conflict', cur.start) if cur.kind == 'list'

          spans << [cur.start, cur.end_pos]
          byte_limit = @limits.bytes.positive? ? @limits.bytes : 65_536
          raise TextError.new('text_limit', cur.start) if repl.bytesize > byte_limit

          probe_limits = TextLimits.new(bytes: byte_limit + 2, allocation: @limits.allocation)
          probe, = SchemaTable.parse_textform_with_comments("(#{repl})".b, probe_limits)
          if probe.children.length != 1 || probe.children[0].kind == 'list' ||
             probe.children[0].start != 1 || probe.children[0].end_pos != repl.bytesize + 1
            raise TextError.new('text_edit_conflict', cur.start)
          end

          candidate = @source.byteslice(0,
                                        cur.start) + repl + @source.byteslice(cur.end_pos,
                                                                              @source.bytesize - cur.end_pos)
          begin
            _, new_val = SchemaTable.load_text_form(@cls, candidate, @limits)
          rescue TextError
            replacements << [cur.start, cur.end_pos, repl]
          else
            replacements << [cur.start, cur.end_pos, repl] if SchemaTable.to_json(new_val) != original
          end
        end
        spans.sort!
        (0...(spans.length - 1)).each do |i|
          prev = spans[i]
          foll = spans[i + 1]
          raise TextError.new('text_edit_conflict', foll[0]) if prev[1] > foll[0] || prev[0] == foll[0]
        end
        replacements.sort_by! { |s, e, _| [s, e] }
        res = @source.b
        replacements.reverse_each do |s, e, r|
          res = res.byteslice(0, s) + r + res.byteslice(e, res.bytesize - e)
        end
        SchemaTable.read_text_form(@cls, res, @limits)
      end
    end

    def self.parse_edit_path(path)
      if path.instance_of?(String)
        raise TextError.new('text_edit_conflict', 0) if path.empty? || path.start_with?('.')

        parts = SchemaTable.arr
        pos = 0
        while pos < path.length
          if pos.positive? && path[pos] != '['
            raise TextError.new('text_edit_conflict', 0) if path[pos] != '.'

            pos += 1
          end
          m = %r{\A([A-Za-z_][A-Za-z_0-9/-]*)|\[([0-9]+)\]}.match(path[pos..])
          raise TextError.new('text_edit_conflict', 0) if m.nil?

          parts << [m[1] || '', m[2] || '']
          pos += m[0].length
        end
        raise TextError.new('text_edit_conflict', 0) if parts.empty?

        parts
      elsif path.is_a?(Array)
        raise TextError.new('text_edit_conflict', 0) if path.empty?

        path.map do |p|
          if p.instance_of?(String) && !p.empty?
            [p, '']
          elsif p.instance_of?(Integer) && !p.negative?
            ['', p.to_s]
          else
            raise TextError.new('text_edit_conflict', 0)
          end
        end
      else
        raise TextError.new('text_edit_conflict', 0)
      end
    end

    def self.text_node_name(node)
      return node.value if node.kind == 'string'
      return node.value[1..] if node.kind == 'keyword'

      raise TextError.new('text_syntax', node.start)
    end

    def self.text_node_pairs(node)
      raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'list'
      raise TextError.new('text_syntax', node.start) if node.children.length.odd?

      out = SchemaTable.arr
      idx = 0
      while idx < node.children.length
        out << [node.children[idx], node.children[idx + 1]]
        idx += 2
      end
      out
    end

    def self.text_node_variant(node)
      raise TextError.new('text_kind_mismatch', node.start) unless %w[string keyword].include?(node.kind)

      text_node_name(node)
    end

    def self.parse_exact_decimal_integer(str, node)
      m = DECIMAL_RE.match(str)
      raise TextError.new('text_kind_mismatch', node.start) if m.nil?

      frac = m[3] || ''
      exp_str = m[4] || '0'
      digits = m[2] + frac
      return 0 if digits.delete('0').empty?

      exp_digits = exp_str.sub(/\A[+-]?0*/, '')
      raise TextError.new('text_bound', node.start) if exp_digits.length > 6

      exp = (exp_digits.empty? ? 0 : exp_digits.to_i) * (exp_str.start_with?('-') ? -1 : 1)
      scale = exp - frac.length
      if scale.negative?
        need_zeros = -scale
        tail = digits.length >= need_zeros ? digits[-need_zeros..] : '1'
        raise TextError.new('text_kind_mismatch', node.start) unless tail.delete('0').empty?

        whole_str = digits[0...(digits.length - need_zeros)]
        val = whole_str.empty? ? 0 : whole_str.to_i
      else
        raise TextError.new('text_bound', node.start) if digits.length + scale > 50

        val = digits.to_i * (10**scale)
      end
      m[1].empty? ? val : -val
    end

    def self.text_node_integer(node, field)
      raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'number'

      val = parse_exact_decimal_integer(node.value, node)
      raise TextError.new('text_bound', node.start) unless val.between?(field.lo, field.hi)

      val.to_s
    end

    def self.resolve_text_record(cls, node, pointer, labels)
      pairs = text_node_pairs(node)
      desc = cls.descriptor
      fields = SchemaTable.map_h
      desc.fields.each { |f| fields[f.json_key] = f }
      seen = SchemaTable.map_h
      parts = SchemaTable.arr
      label = ''
      if !pairs.empty? && pairs[0][0].kind == 'string' && pairs[0][0].value == '&node'
        k_node, i_node = pairs.shift
        tok = i_node.value
        if !pointer || i_node.kind != 'number' || !/\A[1-9][0-9]*\z/.match?(tok) ||
           tok.length > 20 || tok.to_i > 0xFFFFFFFFFFFFFFFF
          raise TextError.new('text_reference', k_node.start)
        end

        label = tok
        if pairs.empty?
          raise TextError.new('text_reference', i_node.start) if labels[label] != desc.name

          return "{\"&node\":#{label}}"
        end
        raise TextError.new('text_reference', i_node.start) if labels.key?(label)

        labels[label] = ''
        parts << "\"&node\":#{label}"
      end
      raw = SchemaTable.map_h
      array_root = desc.json_array && label.empty?
      pairs.each do |k_node, i_node|
        name = text_node_name(k_node)
        raise TextError.new('text_reference', k_node.start) if name.start_with?('&')

        field = fields[name]
        raise TextError.new('text_unknown_key', k_node.start) if field.nil?
        raise TextError.new('text_duplicate_key', k_node.start) if seen[name]

        seen[name] = true
        raw[field.name] = i_node
        resolved = resolve_text_value(field, i_node, labels)
        parts << (array_root ? resolved : "#{json_quote(name)}:#{resolved}")
      end
      desc.fields.each do |field|
        active = true
        field.guards.each do |g_step|
          next unless g_step.length == 2

          g_name = g_step[0].to_s.sub(/\A@/, '')
          wanted = g_step[1]
          g_field = desc.by_name[g_name]
          src = raw[g_name]
          val = src.nil? ? g_field.default_value : (src.kind == 'true')
          active = false if val != wanted
        end
        raise TextError.new('text_inactive_field', raw[field.name].start) if seen[field.json_key] && !active
        raise TextError.new('text_missing_required', node.start) if field.required && active && !seen[field.json_key]
      end
      labels[label] = desc.name unless label.empty?
      return parts[0] || '[]' if array_root

      "{#{parts.join(',')}}"
    end

    def self.resolve_text_value(field, node, labels)
      codec = field.codec
      if node.kind == 'null'
        return 'null' if %w[optional text_optional pointer string_blob bytes_blob].include?(codec)

        raise TextError.new('text_kind_mismatch', node.start)
      end
      return resolve_text_value(json_inner_field(field), node, labels) if codec == 'optional'

      if %w[table pointer].include?(codec)
        cls = by_name(field.target)
        raise TextError.new('text_kind_mismatch', node.start) if cls.nil?

        return resolve_text_record(cls, node, codec == 'pointer', labels)
      end
      if codec == 'union'
        pairs = text_node_pairs(node)
        return '{}' if pairs.empty?

        if pairs.length > 1
          arm_names = pairs.map { |k, _| text_node_name(k) }
          reason = arm_names.uniq.length == arm_names.length ? 'text_kind_mismatch' : 'text_duplicate_key'
          raise TextError.new(reason, node.start)
        end
        k_node, i_node = pairs[0]
        name = text_node_name(k_node)
        un = new_union_by_name(field.target)
        arm = obj_class(un).descriptor.arms.find { |a| a.name == name }
        raise TextError.new('text_unknown_key', k_node.start) if arm.nil?

        if arm.field.nil?
          raise TextError.new('text_kind_mismatch', i_node.start) if i_node.kind != 'null'

          val_str = 'null'
        else
          val_str = resolve_text_value(arm.field, i_node, labels)
        end
        return "{#{json_quote(name)}:#{val_str}}"
      end
      if field.json_flags && !%w[array pointer_array union_array map keyed].include?(codec)
        raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'list'

        names = SchemaTable.arr
        node.children.each do |item|
          nm = text_node_variant(item)
          raise TextError.new('text_unknown_variant', item.start) unless field.json_variants.include?(nm)
          raise TextError.new('text_duplicate_key', item.start) if names.include?(nm)

          names << json_quote(nm)
        end
        return "[#{names.join(',')}]"
      end
      case codec
      when 'enum'
        nm = text_node_variant(node)
        raise TextError.new('text_unknown_variant', node.start) if nm != 'None' && !field.json_variants.include?(nm)

        json_quote(nm)
      when 'keyed'
        seen = SchemaTable.map_h
        parts = SchemaTable.arr
        text_node_pairs(node).each do |k_node, i_node|
          nm = text_node_name(k_node)
          raise TextError.new('text_unknown_key', k_node.start) unless field.json_keys.include?(nm)
          raise TextError.new('text_duplicate_key', k_node.start) if seen[nm]

          seen[nm] = true
          parts << "#{json_quote(nm)}:#{resolve_text_value(field.elem, i_node, labels)}"
        end
        "{#{parts.join(',')}}"
      when 'map'
        raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'list'

        key_field, val_field = json_entry_fields(field)
        seen = SchemaTable.map_h
        parts = SchemaTable.arr
        node.children.each do |pair|
          raise TextError.new('text_kind_mismatch', pair.start) if pair.kind != 'list' || pair.children.length != 2

          k_node, i_node = pair.children
          if key_field.codec == 'string'
            raise TextError.new('text_kind_mismatch', k_node.start) if k_node.kind != 'string'

            nm = k_node.value
            raise TextError.new('text_bound', k_node.start) if !key_field.unbounded && nm.bytesize > key_field.size
          else
            nm = text_node_integer(k_node, key_field)
          end
          raise TextError.new('text_duplicate_key', k_node.start) if seen[nm]

          seen[nm] = true
          parts << "#{json_quote(nm)}:#{resolve_text_value(val_field, i_node, labels)}"
        end
        "{#{parts.join(',')}}"
      when 'array', 'pointer_array', 'union_array'
        raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'list'

        cnt = node.children.length
        if (field.array_kind == 1 && cnt != field.bound) ||
           (field.array_kind != 3 && cnt > field.bound) ||
           (field.array_kind == 2 && cnt < field.array_min)
          raise TextError.new('text_bound', node.start)
        end

        element = json_array_element(field)
        if field.json_text_list && element.codec == 'table'
          cls = by_name(element.target)
          raise TextError.new('text_kind_mismatch', node.start) if cls.nil?

          element = cls.descriptor.fields[0]
        end
        if field.text_total.positive? && %w[string text_optional].include?(element.codec)
          spent = 0
          node.children.each { |ch| spent += ch.value.bytesize if ch.kind == 'string' }
          raise TextError.new('text_bound', node.start) if spent > field.text_total
        end
        "[#{node.children.map { |ch| resolve_text_value(element, ch, labels) }.join(',')}]"
      when 'bool'
        raise TextError.new('text_kind_mismatch', node.start) unless %w[true false].include?(node.kind)

        node.value
      else
        if %w[string text_optional string_blob bytes bytes_blob].include?(codec) || field.kind == 36
          raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'string'

          if %w[bytes bytes_blob].include?(codec)
            begin
              decoded = decode_strict_base64(node.value, field.name)
            rescue Refused
              raise TextError.new('text_encoding', node.start)
            end
            sz = decoded.bytesize
          else
            sz = node.value.bytesize
          end
          if %w[string text_optional
                bytes].include?(codec) && field.size.positive? && !field.unbounded && sz > field.size
            raise TextError.new('text_bound', node.start)
          end

          return json_quote(node.value)
        end
        if codec == 'int' && !FIXED_POINT_KINDS[field.kind]
          v_str = text_node_integer(node, field)
          return WIDE_INTEGER_KINDS[field.kind] ? json_quote(v_str) : v_str
        end
        raise TextError.new('text_kind_mismatch', node.start) if node.kind != 'number'

        WIDE_FIXED_KINDS[field.kind] ? json_quote(node.value) : node.value
      end
    end

    def self.land_text_root(cls, root)
      txt = resolve_text_record(cls, root, false, SchemaTable.map_h)
      begin
        from_json(cls, txt)
      rescue Refused => e
        mapped = {
          'out_of_range' => 'text_bound',
          'length_over_bound' => 'text_bound',
          'count_over_bound' => 'text_bound',
          'timestamp_not_rfc3339' => 'text_timestamp',
          'kind_mismatch' => 'text_kind_mismatch',
          'missing_required' => 'text_missing_required',
          'malformed' => 'text_reference'
        }
        raise TextError.new(mapped.fetch(e.reason, 'text_kind_mismatch'), root.start)
      end
    end

    def self.load_text_form(cls, data, limits = TextLimits.new)
      root, = parse_textform_with_comments(data, limits)
      [root, land_text_root(cls, root)]
    end

    def self.read_text_form(cls, data, limits = TextLimits.new)
      raw = data.is_a?(String) ? data.b : raise(TextError.new('text_syntax', 0))
      root, spans = parse_textform_with_comments(raw, limits)
      TextDocument.new(cls, raw, root, land_text_root(cls, root), limits, spans)
    end

    def self.attach_text_comments(source, root, cls, spans)
      result = SchemaTable.arr
      walk_fn = u(nil)
      walk_fn = lambda do |node, owner, desc, pending|
        slots = SchemaTable.arr
        if desc.is_a?(TableDesc)
          fields = SchemaTable.map_h
          desc.fields.each { |f| fields[f.json_key] = f }
          text_node_pairs(node).each do |k, v|
            nm = text_node_name(k)
            fd = fields[nm]
            slots << [k.start, v.end_pos, owner + [nm], v, fd] if fd
          end
        elsif desc.is_a?(FieldDesc)
          if %w[array pointer_array union_array].include?(desc.codec)
            elem = json_array_element(desc)
            node.children.each_with_index { |v, idx| slots << [v.start, v.end_pos, owner + [idx], v, elem] }
          elsif desc.codec == 'map'
            _, val_fd = json_entry_fields(desc)
            node.children.each do |pair|
              k, v = pair.children
              mk = k.kind == 'number' ? parse_exact_decimal_integer(k.value, k) : k.value
              slots << [pair.start, pair.end_pos, owner + [mk], v, val_fd]
            end
          elsif desc.codec == 'keyed'
            text_node_pairs(node).each do |k, v|
              slots << [k.start, v.end_pos, owner + [text_node_name(k)], v, desc.elem]
            end
          elsif desc.codec == 'union'
            un = new_union_by_name(desc.target)
            text_node_pairs(node).each do |k, v|
              arm = obj_class(un).descriptor.arms.find { |a| a.name == text_node_name(k) }
              slots << [k.start, v.end_pos, owner + [arm.name], v, arm.field]
            end
          elsif desc.json_flags
            node.children.each_with_index { |v, idx| slots << [v.start, v.end_pos, owner + [idx], v, nil] }
          end
        end

        nested = SchemaTable.map_h
        pending.each do |s, e|
          cont_idx = slots.index { |sl| sl[3].kind == 'list' && sl[3].start < s && s < sl[3].end_pos }
          if cont_idx
            (nested[cont_idx] ||= SchemaTable.arr) << [s, e]
            next
          end
          inside = slots.find { |sl| sl[0] < s && s < sl[1] }
          if inside
            result << TextComment.new(s, e, inside[2].freeze)
            next
          end
          prev = slots.reverse.find { |sl| sl[1] <= s }
          after = slots.find { |sl| sl[0] > s }
          between = u(prev ? source.byteslice(prev[1], s - prev[1]) : nil)
          path = if prev && !between.include?("\n".b)
                   prev[2]
                 else
                   after ? after[2] : owner
                 end
          result << TextComment.new(s, e, path.freeze)
        end

        nested.each do |idx, items|
          _, _, path, val_node, fd = slots[idx]
          fd = json_inner_field(fd) while fd.is_a?(FieldDesc) && fd.codec == 'optional'
          child_desc = if fd.is_a?(FieldDesc) && %w[table pointer].include?(fd.codec)
                         tcls = by_name(fd.target)
                         tcls&.descriptor
                       else
                         fd
                       end
          walk_fn.call(val_node, path, child_desc, items)
        end
      end

      inside = SchemaTable.arr
      spans.each do |s, e|
        if root.start < s && s < root.end_pos
          inside << [s, e]
        else
          result << TextComment.new(s, e, SchemaTable::EMPTY_ARR)
        end
      end
      walk_fn.call(root, SchemaTable::EMPTY_ARR, cls.descriptor, inside)
      result.sort_by(&:start).freeze
    end

    def self.locate_text_node(root, cls, parts)
      node = root
      desc = cls.descriptor
      pair_fields = u(nil)
      parts.each do |name, idx_str|
        if pair_fields.nil?
          desc = json_inner_field(desc) while desc.is_a?(FieldDesc) && desc.codec == 'optional'
          if desc.is_a?(FieldDesc) && %w[table pointer].include?(desc.codec)
            tcls = by_name(desc.target)
            raise TextError.new('text_edit_conflict', node.start) if tcls.nil?

            desc = tcls.descriptor
          end
        end
        if name.empty?
          pos = idx_str.to_i
          raise TextError.new('text_edit_conflict', node.start) if node.kind != 'list' || pos >= node.children.length

          if pair_fields
            raise TextError.new('text_edit_conflict', node.start) if pos > 1

            desc = pair_fields[pos]
            pair_fields = u(nil)
          elsif desc.is_a?(FieldDesc) && desc.codec == 'map'
            pair_fields = json_entry_fields(desc)
          elsif desc.is_a?(FieldDesc) && %w[array pointer_array union_array].include?(desc.codec)
            desc = json_array_element(desc)
          elsif desc.is_a?(FieldDesc) && desc.json_flags
            desc = nil
          else
            raise TextError.new('text_edit_conflict', node.start)
          end
          node = node.children[pos]
        else
          fd = if desc.is_a?(TableDesc)
                 desc.fields.find { |f| f.json_key == name }
               elsif desc.is_a?(FieldDesc) && desc.codec == 'keyed' && desc.json_keys.include?(name)
                 desc.elem
               elsif desc.is_a?(FieldDesc) && desc.codec == 'union'
                 arm = union_by_name(desc.target).descriptor.arms.find { |a| a.name == name }
                 arm ? (arm.field || arm) : nil
               end
          if fd.nil? || node.kind != 'list' || node.children.length.odd?
            raise TextError.new('text_edit_conflict',
                                node.start)
          end

          found = text_node_pairs(node).find { |k, _| %w[string keyword].include?(k.kind) && text_node_name(k) == name }
          raise TextError.new('text_edit_conflict', node.start) if found.nil?

          node = found[1]
          desc = fd
        end
      end
      node
    end
  end
end
