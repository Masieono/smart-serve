package httpapi

// csvSafe neutralises a value a spreadsheet would treat as a formula.
//
// `encoding/csv` quotes commas, quotes and newlines, and does nothing about the characters that make
// a cell *executable*: `=`, `+`, `-`, `@`, a leading tab or a carriage return. A drive's model
// string is whatever the firmware says, a USB bridge can be made to report anything, and the exports
// exist to be opened in a spreadsheet, so `=cmd|' /C calc'!A0` in a model name is code that runs on
// the workstation that opens the file. The fix is the one every exporter uses: a leading apostrophe,
// which a spreadsheet reads as "this is text" and a reader still sees as the original string.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// csvRow applies csvSafe to every field of a record.
func csvRow(fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = csvSafe(f)
	}
	return out
}
