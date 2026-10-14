// Package filetext extracts readable plain text from files and applies optional,
// format-independent selections. Read is the simplest whole-file API. Write is
// preferred for large text files because it can decode and filter them as a
// stream without materializing the complete input or output.
package filetext
