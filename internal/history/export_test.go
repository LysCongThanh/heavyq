package history

// DecodeRecord exposes decodeRecord to the external test package.
func DecodeRecord(line []byte) (Record, error) { return decodeRecord(line) }
