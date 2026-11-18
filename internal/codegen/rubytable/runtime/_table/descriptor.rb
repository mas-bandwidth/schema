# frozen_string_literal: true

module SchemaRuntimeUnit
  # Descriptor registry and structural graph helpers.
  module SchemaTable
    IVAR_GET = Kernel.instance_method(:instance_variable_get)
    IVAR_SET = Kernel.instance_method(:instance_variable_set)
    OBJ_CLASS = Kernel.instance_method(:class)
    OBJ_ID = BasicObject.instance_method(:__id__)

    ELEMENT_ZERO = {
      'bool' => false,
      'int' => 0,
      'enum' => 0,
      'float32' => 0.0,
      'float64' => 0.0,
      'string' => ''
    }.freeze

    @registry_by_id = SchemaTable.map_h
    @registry_by_name = SchemaTable.map_h
    @registry_union_by_name = SchemaTable.map_h

    def self.ivar_get(obj, ivar)
      IVAR_GET.bind_call(obj, ivar)
    end

    def self.ivar_set(obj, ivar, val)
      IVAR_SET.bind_call(obj, ivar, val)
    end

    def self.obj_class(val)
      OBJ_CLASS.bind_call(val)
    end

    def self.obj_id(val)
      OBJ_ID.bind_call(val)
    end

    def self.register(cls)
      desc = cls.descriptor
      @registry_by_id[desc.type_id] = cls
      @registry_by_name[desc.name] = cls
    end

    def self.register_value(cls)
      desc = cls.descriptor
      @registry_by_name[desc.name] = cls
    end

    def self.register_union(cls)
      desc = cls.descriptor
      @registry_union_by_name[desc.name] = cls
    end

    def self.by_id(type_id)
      @registry_by_id[type_id]
    end

    def self.by_name(name)
      @registry_by_name[name]
    end

    def self.union_by_name(name)
      @registry_union_by_name[name]
    end

    def self.new_by_name(name)
      cls = @registry_by_name[name]
      raise KeyError, "table #{name} is not registered" if cls.nil?

      cls.new
    end

    def self.new_union_by_name(name)
      cls = @registry_union_by_name[name]
      raise KeyError, "union #{name} is not registered" if cls.nil?

      cls.new
    end

    # Enum-keyed array container (docs/PORT-RUBY.md, Tables: [E]T).
    # Keys are 1..key_max; any non-Integer or out-of-range key raises IndexError.
    class KeyedArray
      attr_reader :key_max, :_s_values

      alias _values _s_values

      def initialize(key_max, &factory)
        @key_max = key_max
        @_s_values = Array.new(key_max) { |i| factory&.call(i + 1) }
      end

      def [](key)
        check_key(key)
        @_s_values[key - 1]
      end

      def []=(key, value)
        check_key(key)
        @_s_values[key - 1] = value
      end

      def length
        @key_max
      end
      alias size length

      def each(&)
        @_s_values.each(&)
      end

      def to_a
        @_s_values.dup
      end

      def ==(other)
        SchemaTable.graph_eq(self, other)
      end

      def inspect
        SchemaTable.graph_inspect(self)
      end

      private

      def check_key(key)
        return if key.instance_of?(Integer) && key >= 1 && key <= @key_max

        raise IndexError, "enum key #{key.inspect} is outside 1..#{@key_max}"
      end
    end

    # Descriptor for one field of a table or hosted packet type.
    class FieldDesc
      attr_accessor :name, :ivar, :json_key, :field_id, :kind, :codec, :inner, :ordinal,
                    :lo, :hi, :f32, :float_range, :fmin, :fmax, :resolution,
                    :target, :map_entry, :pointer, :optional, :array, :array_kind,
                    :array_mode, :array_min, :min_count, :bound, :size, :text_total, :frac_bits,
                    :unbounded, :unbounded_container, :key_enum, :keyed_list, :keys,
                    :elem_kind, :elem_codec, :elem, :element, :variant_ids, :wire_of,
                    :value_of, :deprecated, :default_factory, :required, :guards,
                    :guard, :field_deprecated, :doc, :tags, :type_name, :at, :pieces,
                    :elem_size, :elem_align, :counted, :count_offset, :present_offset,
                    :array_bound, :wide, :variant_names, :key_names, :vocabulary_kind,
                    :enum_max, :json_variants, :json_keys, :json_flags, :json_text_list,
                    :json_text_unbounded
      attr_writer :target_desc, :union_desc

      def initialize(**opts)
        @name = opts.fetch(:name, '')
        @ivar = :"@#{@name}"
        @json_key = opts.fetch(:json_key, @name)
        @field_id = opts.fetch(:field_id, 0)
        @kind = opts.fetch(:kind, 0)
        @codec = opts.fetch(:codec, '')
        @inner = opts.fetch(:inner, '')
        @ordinal = opts.fetch(:ordinal, 0)
        @lo = opts.fetch(:lo, 0)
        @hi = opts.fetch(:hi, 0)
        @f32 = opts.fetch(:f32, false)
        @float_range = opts.fetch(:float_range, false)
        @fmin = opts.fetch(:fmin, 0.0)
        @fmax = opts.fetch(:fmax, 0.0)
        @resolution = opts.fetch(:resolution, 0.0)
        @target = opts.fetch(:target, '')
        @map_entry = opts.fetch(:map_entry, '')
        @pointer = opts.fetch(:pointer, false)
        @optional = opts.fetch(:optional, false)
        @array = opts.fetch(:array, false)
        @array_kind = opts.fetch(:array_kind, @array.is_a?(Integer) ? @array : 0)
        @array_mode = opts.fetch(:array_mode, '')
        @array_min = opts.fetch(:array_min, 0)
        @min_count = opts.fetch(:min_count, @array_min)
        @bound = opts.fetch(:bound, 0)
        @size = opts.fetch(:size, 0)
        @text_total = opts.fetch(:text_total, 0)
        @frac_bits = opts.fetch(:frac_bits, 0)
        @unbounded = opts.fetch(:unbounded, false)
        @unbounded_container = opts.fetch(:unbounded_container, false)
        @key_enum = opts.fetch(:key_enum, '')
        @keyed_list = opts.fetch(:keyed_list, false)
        @keys = opts.fetch(:keys, SchemaTable::EMPTY_ARR)
        @elem_kind = opts.fetch(:elem_kind, 0)
        @elem_codec = opts.fetch(:elem_codec, '')
        @elem = opts.fetch(:elem, nil)
        @variant_ids = opts.fetch(:variant_ids, SchemaTable::EMPTY_ARR)
        dep = opts.fetch(:deprecated, SchemaTable::EMPTY_HASH)
        @deprecated = dep.is_a?(Array) ? dep.to_h { |k| [k, true] }.freeze : dep
        @default_factory = opts.fetch(:default_factory, nil)
        @default_scalar = opts.key?(:default) ? opts[:default] : :_s_unset
        @required = opts.fetch(:required, false)
        @guards = opts.fetch(:guards, SchemaTable::EMPTY_ARR)
        @guard = opts.fetch(:guard, '')
        @field_deprecated = opts.fetch(:field_deprecated, false)
        @doc = opts.fetch(:doc, '')
        @tags = opts.fetch(:tags, SchemaTable::EMPTY_ARR)
        @type_name = opts.fetch(:type_name, '')
        @at = opts.fetch(:at, 0)
        @pieces = opts.fetch(:pieces, SchemaTable::EMPTY_ARR)
        @elem_size = opts.fetch(:elem_size, 0)
        @elem_align = opts.fetch(:elem_align, 1)
        @counted = opts.fetch(:counted, false)
        @count_offset = opts.fetch(:count_offset, -1)
        @present_offset = opts.fetch(:present_offset, -1)
        @array_bound = opts.fetch(:array_bound, @bound)
        @wide = opts.fetch(:wide, nil)
        @variant_names = opts.fetch(:variant_names, SchemaTable::EMPTY_ARR)
        @key_names = opts.fetch(:key_names, SchemaTable::EMPTY_ARR)
        @vocabulary_kind = opts.fetch(:vocabulary_kind, '')
        @enum_max = opts.fetch(:enum_max, 0)
        @json_variants = opts.fetch(:json_variants, SchemaTable::EMPTY_ARR)
        @json_keys = opts.fetch(:json_keys, SchemaTable::EMPTY_ARR)
        @json_flags = opts.fetch(:json_flags, false)
        @json_text_list = opts.fetch(:json_text_list, false)
        @json_text_unbounded = opts.fetch(:json_text_unbounded, false)
        @target_desc = nil
        @union_desc = nil
        build_enum_maps
        @element = if opts.key?(:element)
                     opts[:element]
                   elsif @array && @array != 0 && !@elem_codec.empty?
                     FieldDesc.new(
                       name: @name,
                       field_id: 0,
                       kind: @elem_kind,
                       codec: @elem_codec,
                       lo: @lo,
                       hi: @hi,
                       target: @target,
                       pointer: @pointer,
                       size: @size,
                       variant_ids: @variant_ids,
                       variant_names: @variant_names,
                       vocabulary_kind: @vocabulary_kind,
                       enum_max: @enum_max,
                       deprecated: @deprecated,
                       default: ELEMENT_ZERO[@elem_codec],
                       f32: @f32,
                       fmin: @fmin,
                       fmax: @fmax,
                       float_range: @float_range,
                       frac_bits: @frac_bits,
                       json_variants: @json_variants,
                       json_flags: @json_flags,
                       unbounded: @json_text_list ? @json_text_unbounded : @unbounded,
                       wide: @wide
                     )
                   else
                     @elem
                   end
      end

      def build_enum_maps
        @wire_of = SchemaTable.map_h
        @value_of = SchemaTable.map_h
        @variant_ids.each_with_index do |wid, idx|
          ord = idx + 1
          @wire_of[ord] = wid
          @value_of[wid] = ord
        end
        @wire_of.freeze
        @value_of.freeze
      end

      def default_value
        return @default_factory.call unless @default_factory.nil?
        return @default_scalar unless @default_scalar.equal?(:_s_unset)

        nil
      end
      alias default default_value

      def guard_holds?(owner)
        return true if @guards.empty?

        cur = owner
        @guards.each do |step|
          if step.length == 2
            cond_ivar = step[0].is_a?(Symbol) && step[0].to_s.start_with?('@') ? step[0] : :"@#{step[0]}"
            return false if SchemaTable.ivar_get(owner, cond_ivar).equal?(true) != step[1]
          else
            case step[0]
            when :field
              cur = SchemaTable.ivar_get(cur, :"@#{step[1]}")
              return false if cur.nil?
            when :arm
              return false if cur.nil? || cur.type != step[1]

              cur = SchemaTable.ivar_get(cur, :"@#{step[2]}") if step[2]
            when :eq
              return false unless SchemaTable.ivar_get(cur, :"@#{step[1]}") == step[2]
            when :in
              return false unless step[2].include?(SchemaTable.ivar_get(cur, :"@#{step[1]}"))
            end
          end
        end
        true
      end
      alias guard_holds guard_holds?

      def default?(value)
        if @codec == 'pointer_array'
          return false unless value.instance_of?(Array)
          return value.all?(&:nil?) if @array_mode == 'fixed'
          return value.length == @min_count && value.all?(&:nil?) if @array_mode == 'counted'

          return value.empty?
        end
        return value.nil? if @pointer || @optional || @codec == 'string_blob' || @codec == 'bytes_blob'
        return value.instance_of?(Hash) && value.empty? if @codec == 'map'

        def_val = default_value
        case @codec
        when 'bool', 'int', 'enum', 'unsupported'
          value == def_val
        when 'string'
          value.instance_of?(String) && def_val.instance_of?(String) &&
            TEXT_ENCODINGS.include?(value.encoding) && value.bytes == def_val.bytes
        when 'bytes'
          value.instance_of?(String) && def_val.instance_of?(String) &&
            value.encoding == Encoding::ASCII_8BIT && value.bytes == def_val.bytes
        when 'float32'
          return false unless value.instance_of?(Float)
          return SchemaTable.float32_round(value) == SchemaTable.float32_round(def_val) if @float_range

          SchemaTable.float32_bits_eq?(value, def_val)
        when 'float64'
          return false unless value.instance_of?(Float)
          return value == def_val if @float_range

          SchemaTable.float64_bits_eq?(value, def_val)
        when 'table'
          return true if value.nil?
          return false if @target.empty?

          vdesc = SchemaTable.obj_class(value).descriptor
          vdesc.fields.all? do |f|
            !f.guard_holds?(value) || (!f.required && f.default?(f.get(value)))
          end
        when 'union'
          value.nil? || value.type.zero?
        when 'array'
          if @array_kind == 1
            return false if @elem_kind == 13 || @element.nil?

            return value.all? { |item| @element.default?(item) }
          end
          value.empty?
        when 'union_array'
          return value.all? { |el| el.type.zero? } if @array_kind == 1

          value.length == @array_min && value.all? { |el| el.type.zero? }
        when 'keyed'
          slots = @keyed_list ? value : value._s_values
          !@elem.nil? && slots.all? { |s| @elem.default?(s) }
        else
          SchemaTable.graph_eq(value, def_val, self)
        end
      end
      alias is_default default?

      def get(record)
        SchemaTable.ivar_get(record, @ivar)
      end

      def set(record, value)
        SchemaTable.ivar_set(record, @ivar, value)
      end

      def get_element(record, index)
        get(record)[index]
      end

      def set_element(record, index, elem_value)
        val = get(record)
        if val.instance_of?(String)
          if val.encoding == Encoding::ASCII_8BIT
            dup = val.b
            dup.setbyte(index, elem_value)
            set(record, dup)
          else
            chars = val.chars
            chars[index] = elem_value
            set(record, chars.join)
          end
        else
          val[index] = elem_value
        end
      end

      def variant_name(value)
        return nil if @vocabulary_kind.empty?

        if @vocabulary_kind == 'flags'
          return value >= 0 && value < @variant_names.length ? @variant_names[value] : nil
        end
        return 'None' if value.zero? && (@vocabulary_kind == 'enum' || @vocabulary_kind == 'union')
        return @variant_names[value - 1] if value.between?(1, @variant_names.length)

        nil
      end

      def variant_id(value)
        return nil unless @vocabulary_kind == 'enum' || @vocabulary_kind == 'union'
        return 0 if value <= 0 || value > @variant_ids.length

        @variant_ids[value - 1]
      end

      def key_name(key)
        return nil if @key_enum.empty?
        return 'None' if key.zero?

        if key.between?(1, @key_names.length)
          nm = @key_names[key - 1]
          return nm.empty? ? nil : nm
        end

        nil
      end

      def key_id(key)
        return nil if @key_enum.empty?
        return @keys[key - 1] if key.between?(1, @keys.length)

        0
      end

      def target_desc
        nm = @map_entry.empty? ? @target : @map_entry
        cls = SchemaTable.by_name(nm)
        cls&.descriptor
      end

      def union_desc
        cls = SchemaTable.union_by_name(@target)
        cls&.descriptor
      end
    end

    # Descriptor for one arm of a union.
    class ArmDesc
      attr_reader :name, :ivar, :json_key, :wire_id, :tag, :kind, :target, :pointer, :size, :field, :doc, :tags,
                  :deprecated
      attr_accessor :target_desc

      alias arm_id wire_id

      def initialize(name:, wire_id:, tag:, kind:, field: nil, json_key: nil, doc: '', tags: SchemaTable::EMPTY_ARR,
                     size: 0, deprecated: false, target: '', pointer: false)
        @name = name
        @ivar = :"@#{name}"
        @json_key = json_key || name
        @wire_id = wire_id
        @tag = tag
        @kind = kind
        @target = target
        @pointer = pointer
        @size = size
        @field = field
        @doc = doc
        @tags = tags
        @deprecated = deprecated
        @target_desc = nil
      end
    end

    # Descriptor for a union declaration.
    class UnionDesc
      attr_reader :name, :arms, :by_id, :by_tag, :size, :tag_size, :arm_offset, :doc, :tags

      def initialize(name:, arms:, size: 0, tag_size: 1, arm_offset: 0, doc: '', tags: SchemaTable::EMPTY_ARR)
        @name = name
        @arms = arms.freeze
        @size = size
        @tag_size = tag_size
        @arm_offset = arm_offset
        @doc = doc
        @tags = tags
        by_id = SchemaTable.map_h
        by_tag = SchemaTable.map_h
        @arms.each do |arm|
          by_id[arm.wire_id] = arm
          by_tag[arm.tag] = arm
        end
        @by_id = by_id.freeze
        @by_tag = by_tag.freeze
      end
    end

    # Descriptor for a table or hosted packet type.
    class TableDesc
      attr_reader :name, :doc, :tags, :table, :type_id, :variable, :fixed, :integrity,
                  :json_array, :forms, :size, :align, :cook_align, :extent_start,
                  :build_version, :region_align, :bytes_blob_type_id, :string_blob_type_id,
                  :padding, :fields, :by_id, :by_name, :guards, :reset

      def initialize(**opts)
        @name = opts.fetch(:name)
        @doc = opts.fetch(:doc, '')
        @tags = opts.fetch(:tags, SchemaTable::EMPTY_ARR)
        @table = opts.fetch(:table, true)
        @type_id = opts.fetch(:type_id, 0)
        @variable = opts.fetch(:variable, false)
        @fixed = opts.fetch(:fixed, false)
        @integrity = opts.fetch(:integrity, '')
        @json_array = opts.fetch(:json_array, false)
        @forms = opts.fetch(:forms, SchemaTable::EMPTY_ARR)
        @size = opts.fetch(:size, 0)
        @align = opts.fetch(:align, 1)
        @cook_align = opts.fetch(:cook_align, 1)
        @extent_start = opts.fetch(:extent_start, 0)
        @build_version = opts.fetch(:build_version, 0)
        @region_align = opts.fetch(:region_align, 8)
        @bytes_blob_type_id = opts.fetch(:bytes_blob_type_id, 0x2F2EC0474F1C4FE4)
        @string_blob_type_id = opts.fetch(:string_blob_type_id, 0x704BE0D8FAAFFC58)
        @padding = opts.fetch(:padding, SchemaTable::EMPTY_ARR)
        @fields = opts.fetch(:fields, SchemaTable.arr).freeze
        @guards = opts.fetch(:guards, SchemaTable::EMPTY_ARR)
        @reset = opts.fetch(:reset, ->(v) { v._s_reset if v.respond_to?(:_s_reset) })
        by_id = SchemaTable.map_h
        by_name = SchemaTable.map_h
        @fields.each do |f|
          by_id[f.field_id] = f
          by_name[f.name] = f
        end
        @by_id = by_id.freeze
        @by_name = by_name.freeze
      end
    end

    # Block record field descriptor (SPEC-TABLES §19.2, §19.3).
    class BlockField
      attr_reader :name, :kind, :offset, :size, :out_of_line, :counted, :optional,
                  :array, :bound, :elem_size, :count_offset, :present_offset,
                  :target, :offset_of_offset, :stride_offset, :stride

      def initialize(name:, kind:, offset:, size:, **opts)
        @name = name
        @kind = kind
        @offset = offset
        @size = size
        @out_of_line = opts.fetch(:out_of_line, false)
        @counted = opts.fetch(:counted, false)
        @optional = opts.fetch(:optional, false)
        @array = opts.fetch(:array, false)
        @bound = opts.fetch(:bound, 0)
        @elem_size = opts.fetch(:elem_size, 0)
        @count_offset = opts.fetch(:count_offset, -1)
        @present_offset = opts.fetch(:present_offset, -1)
        @target = opts.fetch(:target, '')
        @offset_of_offset = opts.fetch(:offset_of_offset, -1)
        @stride_offset = opts.fetch(:stride_offset, -1)
        @stride = opts.fetch(:stride, 0)
      end
    end

    # Block record layout descriptor.
    class BlockRecord
      attr_reader :name, :size, :align, :fields

      def initialize(name:, size:, align:, fields:)
        @name = name
        @size = size
        @align = align
        @fields = fields.freeze
      end
    end

    # Block out-of-line array descriptor.
    class BlockArray
      attr_reader :name, :offset, :maximum, :stride, :elem_align, :element

      def initialize(name:, offset:, maximum:, stride:, elem_align:, element:)
        @name = name
        @offset = offset
        @maximum = maximum
        @stride = stride
        @elem_align = elem_align
        @element = element
      end
    end

    # Block root table descriptor.
    class BlockTable
      attr_reader :name, :projection, :arrays, :max_bytes

      def initialize(name:, projection:, arrays:, max_bytes:)
        @name = name
        @projection = projection
        @arrays = arrays.freeze
        @max_bytes = max_bytes
      end
    end

    def self.generated_object?(val)
      return true if val.is_a?(KeyedArray)
      return false if val.nil? || val.equal?(true) || val.equal?(false) || val.is_a?(Numeric) || val.is_a?(String)
      return false if val.is_a?(Array) || val.is_a?(Hash) || val.is_a?(BlobRef)

      cls = obj_class(val)
      cls.respond_to?(:descriptor) && (cls.descriptor.is_a?(TableDesc) || cls.descriptor.is_a?(UnionDesc))
    end

    # Iterative structural equality over cyclic/deep graphs, comparing floats by bits.
    def self.graph_eq(lhs, rhs, root_field = nil)
      assumed = SchemaTable.map_h
      stack = [[lhs, rhs, root_field]]
      until stack.empty?
        x, y, fd = stack.pop
        next if x.equal?(y)

        if x.is_a?(BlobRef) || y.is_a?(BlobRef)
          return false unless x == y

          next
        end
        return false unless obj_class(x) == obj_class(y)

        if x.instance_of?(Float)
          eq = if fd&.f32 || fd&.kind == 10
                 float32_bits_eq?(x, y)
               else
                 float64_bits_eq?(x, y)
               end
          return false unless eq

          next
        end

        if x.instance_of?(String)
          return false unless (x.encoding == Encoding::ASCII_8BIT) == (y.encoding == Encoding::ASCII_8BIT)
          return false unless x.bytes == y.bytes

          next
        end

        if x.is_a?(KeyedArray)
          pair = [obj_id(x), obj_id(y), 0]
          next if assumed[pair]

          assumed[pair] = true
          return false unless x.key_max == y.key_max

          elem_fd = fd&.elem
          x._s_values.each_with_index do |xv, idx|
            stack << [xv, y._s_values[idx], elem_fd]
          end
          next
        end

        if generated_object?(x)
          pair = [obj_id(x), obj_id(y), 0]
          next if assumed[pair]

          assumed[pair] = true
          desc = obj_class(x).descriptor
          if desc.is_a?(TableDesc)
            desc.fields.each do |child_fd|
              stack << [child_fd.get(x), child_fd.get(y), child_fd]
            end
          elsif desc.is_a?(UnionDesc)
            return false unless x.type == y.type

            if x.type.positive?
              arm = desc.by_tag[x.type]
              stack << [ivar_get(x, arm.ivar), ivar_get(y, arm.ivar), arm.field] if arm&.field
            end
          end
          next
        end

        if x.instance_of?(Array)
          pair = [obj_id(x), obj_id(y), fd.object_id]
          next if assumed[pair]

          assumed[pair] = true
          return false unless x.length == y.length

          elem_fd = fd&.element || fd&.elem
          x.each_with_index do |xv, idx|
            stack << [xv, y[idx], elem_fd]
          end
          next
        end

        if x.instance_of?(Hash)
          pair = [obj_id(x), obj_id(y), fd.object_id]
          next if assumed[pair]

          assumed[pair] = true
          return false unless x.length == y.length

          val_fd = nil
          if fd&.target && !fd.target.empty?
            entry_cls = by_name(fd.target)
            val_fd = entry_cls.descriptor.fields[1] if entry_cls
          end
          x.each do |k, xv|
            return false unless y.key?(k)

            stack << [xv, y[k], val_fd]
          end
          next
        end

        return false unless x == y
      end
      true
    end

    # Iterative bounded inspect over cyclic/deep graphs.
    def self.graph_inspect(root)
      seen = SchemaTable.map_h
      out = +''
      stack = [[:visit, root]]
      until stack.empty?
        op, val = stack.pop
        if op == :lit
          out << val
          next
        end
        if val.is_a?(KeyedArray)
          oid = obj_id(val)
          if seen[oid]
            out << 'KeyedArray(...)'
            next
          end
          seen[oid] = true
          out << 'KeyedArray['
          stack << [:lit, ']']
          vals = val._s_values
          (vals.length - 1).downto(0) do |i|
            stack << [:lit, ', '] if i < vals.length - 1
            stack << [:visit, vals[i]]
          end
        elsif generated_object?(val)
          oid = obj_id(val)
          cls = obj_class(val)
          if seen[oid]
            out << "#{cls.descriptor.name}(...)"
            next
          end
          seen[oid] = true
          desc = cls.descriptor
          out << "#{desc.name}("
          stack << [:lit, ')']
          if desc.is_a?(TableDesc)
            fields = desc.fields
            (fields.length - 1).downto(0) do |i|
              f = fields[i]
              stack << [:lit, ', '] if i < fields.length - 1
              stack << [:visit, f.get(val)]
              stack << [:lit, "#{f.name}: "]
            end
          elsif desc.is_a?(UnionDesc)
            if val.type.positive? && (arm = desc.by_tag[val.type]) && arm.field
              stack << [:visit, ivar_get(val, arm.ivar)]
              stack << [:lit, ", #{arm.name}: "]
            end
            stack << [:lit, "type: #{val.type}"]
          end
        elsif val.instance_of?(Array)
          oid = obj_id(val)
          if seen[oid]
            out << '[...]'
            next
          end
          seen[oid] = true
          out << '['
          stack << [:lit, ']']
          (val.length - 1).downto(0) do |i|
            stack << [:lit, ', '] if i < val.length - 1
            stack << [:visit, val[i]]
          end
        elsif val.instance_of?(Hash)
          oid = obj_id(val)
          if seen[oid]
            out << '{...}'
            next
          end
          seen[oid] = true
          out << '{'
          stack << [:lit, '}']
          entries = val.to_a
          (entries.length - 1).downto(0) do |i|
            k, v = entries[i]
            stack << [:lit, ', '] if i < entries.length - 1
            stack << [:visit, v]
            stack << [:lit, ' => ']
            stack << [:visit, k]
          end
        else
          out << val.inspect
        end
      end
      out
    end
  end
end
