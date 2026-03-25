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

// assignSequentialOriginalExcelRows sets original_excel_row to 1..N for every row (used after merging files).
func assignSequentialOriginalExcelRows(data *CSVData) (*CSVData, error) {
	if data == nil {
		return nil, fmt.Errorf("nil CSVData")
	}

	out := data.clone()
	idx := out.columnIndexByName("original_excel_row")
	if idx == -1 {
		out.Columns = append(out.Columns, "original_excel_row")
		idx = len(out.Columns) - 1
		for i := range out.Rows {
			out.Rows[i] = append(out.Rows[i], strconv.Itoa(i+1))
		}
		return out, nil
	}

	for i := range out.Rows {
		if idx >= len(out.Rows[i]) {
			padded := make([]string, idx+1)
			copy(padded, out.Rows[i])
			out.Rows[i] = padded
		}
		out.Rows[i][idx] = strconv.Itoa(i + 1)
	}

	return out, nil
}

func excludeRowsByOriginalExcelRow(data *CSVData, excluded []int) (*CSVData, int, error) {
	if data == nil {
		return nil, 0, fmt.Errorf("nil CSVData")
	}
	if len(excluded) == 0 {
		return data.clone(), 0, nil
	}

	idx := data.columnIndexByName("original_excel_row")
	if idx < 0 {
		return nil, 0, fmt.Errorf("missing original_excel_row column")
	}

	excludedSet := make(map[int]struct{}, len(excluded))
	for _, rowID := range excluded {
		excludedSet[rowID] = struct{}{}
	}

	out := &CSVData{
		Columns:        append([]string(nil), data.Columns...),
		Rows:           make([][]string, 0, len(data.Rows)),
		FileInfo:       data.FileInfo,
		InputRadioTech: data.InputRadioTech,
	}
	removed := 0

	for _, row := range data.Rows {
		rowID, err := originalExcelRowValue(row, idx)
		if err != nil {
			return nil, 0, err
		}
		if _, skip := excludedSet[rowID]; skip {
			removed++
			continue
		}
		out.Rows = append(out.Rows, append([]string(nil), row...))
	}

	return out, removed, nil
}

func originalExcelRowValue(row []string, idx int) (int, error) {
	if idx < 0 || idx >= len(row) {
		return 0, fmt.Errorf("row is missing original_excel_row value")
	}
	value := strings.TrimSpace(row[idx])
	rowID, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid original_excel_row value %q", value)
	}
	return rowID, nil
}
