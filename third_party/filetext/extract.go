package filetext

import "fmt"

// IsSupported reports whether path has a recognized name or extension.
// Unknown names can still be read when content detection identifies text.
func IsSupported(path string) bool {
	_, ok := formatFromPath(path)
	return ok
}

// ReadAllText detects the format and extracts all readable text.
func ReadAllText(path string) (string, error) {
	format, err := DetectFormat(path)
	if err != nil {
		return "", err
	}
	return readAllTextAs(path, format)
}

func readAllTextAs(path string, format Format) (string, error) {
	switch format {
	case FormatText:
		return ReadTextFile(path)
	case FormatHTML:
		return ExtractTextFromHTML(path)
	case FormatXML:
		return ExtractTextFromXML(path)
	case FormatJSON:
		return ExtractTextFromJSON(path)
	case FormatJSONL:
		return ExtractTextFromJSONLines(path)
	case FormatYAML:
		return ExtractTextFromYAML(path)
	case FormatTOML:
		return ExtractTextFromTOML(path)
	case FormatINI:
		return ExtractTextFromINI(path)
	case FormatCSV:
		return ExtractTextFromCSV(path)
	case FormatTSV:
		return ExtractTextFromTSV(path)
	case FormatDOCX:
		return ExtractTextFromDocx(path)
	case FormatPPTX:
		return ExtractTextFromPPTX(path)
	case FormatXLSX:
		return ExtractTextFromXLSX(path)
	case FormatODF:
		return ExtractTextFromODF(path)
	case FormatPDF:
		return ExtractTextFromPDF(path)
	case FormatRTF:
		return ExtractTextFromRTF(path)
	case FormatEPUB:
		return ExtractTextFromEPUB(path)
	case FormatSRT:
		return ExtractTextFromSRT(path)
	case FormatVTT:
		return ExtractTextFromVTT(path)
	case FormatTeX:
		return ExtractTextFromTEX(path)
	case FormatBibTeX:
		return ExtractTextFromBIB(path)
	case FormatEmail:
		return ExtractTextFromEmail(path)
	case FormatZIP:
		return ExtractTextFromZIP(path)
	case FormatTAR:
		return ExtractTextFromTAR(path)
	case FormatGZIP:
		return ExtractTextFromGZIP(path)
	case FormatBZIP2:
		return ExtractTextFromBZIP2(path)
	case FormatBinary:
		return ExtractTextFromBinary(path)
	default:
		return "", fmt.Errorf("filetext: unsupported parser %q", format)
	}
}
