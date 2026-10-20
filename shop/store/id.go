package store

// NewID returns a new module identifier (UUID v4 shaped) for callers outside
// this package.
func NewID() string { return newID() }
