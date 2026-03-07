package backend

import (
	"fmt"
	"strconv"
	"strings"
)

func ensureOriginalExcelRowColumn(data *CSVData) (*CSVData, error) {
	if data == nil {
		return nil, fmt.Errorf("nil CSVData")
	}

	out := data.clone()
	idx := out.columnIndexByName("original_excel_row")
	if idx == -1 {
		out.Columns = append(out.Columns, "original_excel_row")
		idx = len(out.Columns) - 1
		for i := range out.Rows {
			out.Rows[i] = append(out.Rows[i], strconv.Itoa(i+out.FileInfo.HeaderLine+1))
		}
		return out, nil
	}

	for i := range out.Rows {
		if idx >= len(out.Rows[i]) {
			padded := make([]string, idx+1)
			copy(padded, out.Rows[i])
			out.Rows[i] = padded
		}
		if strings.TrimSpace(out.Rows[i][idx]) == "" {
			out.Rows[i][idx] = strconv.Itoa(i + out.FileInfo.HeaderLine + 1)
		}
	}

	return out, nil
}
