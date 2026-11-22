# frozen_string_literal: true

# The Ruby conformance driver (test/conformance/README.md; docs/PORT-RUBY.md).
#
# The driver script execs this file once per surface. A surface is a Ruby script
# in surfaces/ named for the surface; list prints the ones present. The
# generated tree is RUBY_CONFORMANCE_GENERATED, default build/conformance-ruby,
# so a negative control can point the same driver at a sabotaged copy.

require 'fileutils'

module RubyConformanceMain
  HERE = File.expand_path(__dir__ || '.')
  SURFACES = File.join(HERE, 'surfaces')

  def self.surface_names
    return [] unless File.directory?(SURFACES)

    Dir.children(SURFACES).select { |n| n.end_with?('.rb') && !n.start_with?('_') }
       .map { |n| n.delete_suffix('.rb') }
       .sort
  end

  def self.main(argv)
    if argv.length == 2 && argv[1] == 'list'
      surface_names.each { |name| puts name }
      return 0
    end
    if argv.length == 3
      manifest, name, outdir = argv
      path = File.join(SURFACES, "#{name}.rb")
      return 2 unless File.file?(path)

      FileUtils.mkdir_p(outdir)
      $LOAD_PATH.unshift(SURFACES) unless $LOAD_PATH.include?(SURFACES)
      require_relative 'surfaces/_common'
      RubyConformance.surface = name
      mod = Module.new
      mod.module_eval(File.read(path, encoding: 'UTF-8'), path, 1)
      mod.run(manifest, outdir)
      # every case ran; one that hit a driver error reds the surface
      return RubyConformance.driver_errors.positive? ? 1 : 0
    end
    $stderr.write("usage: main.rb <manifest> list\n       main.rb <manifest> <surface> <outdir>\n")
    2
  end
end

exit(RubyConformanceMain.main(ARGV))
