package backend

import "fmt"

func LoadTimeSelectorData(filePath string) (TimeSelectorData, error) {
	data, err := LoadCSVFile(filePath)
	if err != nil {
		return TimeSelectorData{}, err
	}
	data, err = ensureOriginalExcelRowColumn(data)
	if err != nil {
		return TimeSelectorData{}, err
	}

	utcCol := findColumnNameNative(data.Columns, []string{"UTC"})
	dateCol := findColumnNameNative(data.Columns, []string{"Date"})
	timeCol := findColumnNameNative(data.Columns, []string{"Time"})

	utcIdx := data.columnIndexByName(utcCol)
	dateIdx := data.columnIndexByName(dateCol)
	timeIdx := data.columnIndexByName(timeCol)
	origIdx := data.columnIndexByName("original_excel_row")
	if origIdx < 0 {
		return TimeSelectorData{}, fmt.Errorf("missing original_excel_row column")
	}

	series, strategy := buildTimeMillisSeriesNative(data, utcIdx, dateIdx, timeIdx)
	rows := make([]TimeSelectorRow, 0, len(data.Rows))
	var minTimeMS int64
	var maxTimeMS int64
	hasValidTime := false

	for i, row := range data.Rows {
		if !series.Valid[i] {
			continue
		}
		rowID, err := originalExcelRowValue(row, origIdx)
		if err != nil {
			return TimeSelectorData{}, err
		}
		ts := series.Values[i]
		if !hasValidTime {
			minTimeMS = ts
			maxTimeMS = ts
			hasValidTime = true
		} else {
			if ts < minTimeMS {
				minTimeMS = ts
			}
			if ts > maxTimeMS {
				maxTimeMS = ts
			}
		}
		rows = append(rows, TimeSelectorRow{
			OriginalRow: rowID,
			TimestampMS: ts,
		})
	}

	if !hasValidTime {
		return TimeSelectorData{}, fmt.Errorf("v súbore sa nenašli použiteľné časové údaje (UTC alebo Date + Time)")
	}

	return TimeSelectorData{
		Rows:      rows,
		TotalRows: len(data.Rows),
		TimedRows: len(rows),
		MinTimeMS: minTimeMS,
		MaxTimeMS: maxTimeMS,
		Strategy:  strategy,
	}, nil
}
