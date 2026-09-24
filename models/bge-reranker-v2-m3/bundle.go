// Package bgereranker contains the small, distributable part of TANAMORI's
// primary model integration. The multi-gigabyte model weights are installed
// into the user's data directory by the first-run setup.
package bgereranker

import _ "embed"

//go:embed manifest.json
var manifest []byte

//go:embed runtime.py
var runtime []byte

// Manifest returns a copy of the pinned model manifest.
func Manifest() []byte {
	return append([]byte(nil), manifest...)
}

// Runtime returns a copy of the Python inference worker.
func Runtime() []byte {
	return append([]byte(nil), runtime...)
}
