#!/usr/bin/env ruby

require "json"
require "yaml"

file_path = ARGV[0]
service_name = ARGV[1]

abort "usage: render_sync_api_doc.rb <file_path> <service_name>" if file_path.to_s.empty? || service_name.to_s.empty?

raw = File.read(file_path)
ext = File.extname(file_path).downcase

doc = if ext == ".json"
  JSON.parse(raw)
else
  YAML.safe_load(raw, aliases: true) || {}
end

overrides_raw = ENV.fetch("API_DOC_META_OVERRIDES_JSON", "").strip
overrides = overrides_raw.empty? ? {} : JSON.parse(overrides_raw)
default_meta = overrides.fetch("default", {})
service_meta = overrides.fetch(service_name, {})
meta = default_meta.merge(service_meta)

doc["host"] = meta["host"] if meta.key?("host")
doc["basePath"] = meta["basePath"] if meta.key?("basePath")
if meta.key?("schemes")
  schemes = meta["schemes"]
  doc["schemes"] = schemes.is_a?(Array) ? schemes : [schemes]
end

print JSON.generate(doc)
