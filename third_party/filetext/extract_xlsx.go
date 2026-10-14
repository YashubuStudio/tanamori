package filetext

import (
	"strings"

	"github.com/xuri/excelize/v2"
)

func ExtractTextFromXLSX(path string) (string, error) {
	workbook, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer workbook.Close()

	var output strings.Builder
	first := true
	for _, sheet := range workbook.GetSheetList() {
		rows, err := workbook.Rows(sheet)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			columns, err := rows.Columns()
			if err != nil {
				rows.Close()
				return "", err
			}
			if !first {
				output.WriteByte('\n')
			}
			first = false
			output.WriteString(strings.Join(columns, "\t"))
		}
		if err := rows.Close(); err != nil {
			return "", err
		}
	}
	return output.String(), nil
}
