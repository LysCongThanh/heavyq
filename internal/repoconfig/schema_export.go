package repoconfig

// FileLayout is the TOML layout of heavyq.toml as written, before validation: [[gate]], [[pool]] and
// [anchor] with their keys. It is exported only so that tools/schemagen, a separate dev-only module,
// can generate the JSON Schema of heavyq.toml from the type the parser really uses.
type FileLayout = rawFile
