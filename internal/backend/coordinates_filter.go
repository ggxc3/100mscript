package backend

import "fmt"

func filterRowsWithoutGPS(data *CSVData, columnMapping map[string]int) (*CSVData, int, error) {
	if data == nil {
		return nil, 0, fmt.Errorf("nil CSVData")
	}
	if columnMapping == nil {
		return nil, 0, fmt.Errorf("missing column mapping")
	}

	latIdx, okLat := columnMapping["latitude"]
	lonIdx, okLon := columnMapping["longitude"]
	if !okLat || !okLon {
		return nil, 0, fmt.Errorf("missing required column mapping: latitude/longitude")
	}
	if latIdx < 0 || latIdx >= len(data.Columns) || lonIdx < 0 || lonIdx >= len(data.Columns) {
		return nil, 0, fmt.Errorf("invalid latitude/longitude column mapping index")
	}

	out := &CSVData{
		Columns:  append([]string(nil), data.Columns...),
		Rows:     make([][]string, 0, len(data.Rows)),
		FileInfo: data.FileInfo,
	}
	skipped := 0
	for _, row := range data.Rows {
		if _, ok := parseNumberString(cellAt(row, latIdx)); !ok {
			skipped++
			continue
		}
		if _, ok := parseNumberString(cellAt(row, lonIdx)); !ok {
			skipped++
			continue
		}
		out.Rows = append(out.Rows, append([]string(nil), row...))
	}

	return out, skipped, nil
}
