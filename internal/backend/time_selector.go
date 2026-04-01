package backend

import (
	"context"
	"fmt"
	"strings"
)

func LoadTimeSelectorData(paths []string) (TimeSelectorData, error) {
	paths = NormalizeInputPaths(paths)
	if len(paths) == 0 {
		return TimeSelectorData{}, fmt.Errorf("žiadna cesta k CSV súboru")
	}

	var data *CSVData
	var err error
	if len(paths) == 1 {
		data, err = LoadCSVFile(paths[0])
		if err != nil {
			return TimeSelectorData{}, err
		}
		data, err = ensureOriginalExcelRowColumn(data)
	} else {
		data, err = LoadAndMergeCSVFiles(context.Background(), paths)
		if err != nil {
			return TimeSelectorData{}, err
		}
		if sorted, ok := sortMergedCSVRowsByTime(data); ok {
			data = sorted
		}
		data, err = assignSequentialOriginalExcelRows(data)
	}
	if err != nil {
		return TimeSelectorData{}, err
	}

	return buildTimeSelectorFromCSVData(data)
}

func buildTimeSelectorFromCSVData(data *CSVData) (TimeSelectorData, error) {
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

	series, strategy := buildTimeSelectorSeriesNative(data, utcIdx, dateIdx, timeIdx)
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

// timeSeriesForSorting uses the same UTC / Date+Time column resolution as the time-window UI.
func timeSeriesForSorting(data *CSVData) (timeSeriesNative, string) {
	utcCol := findColumnNameNative(data.Columns, []string{"UTC"})
	dateCol := findColumnNameNative(data.Columns, []string{"Date"})
	timeCol := findColumnNameNative(data.Columns, []string{"Time"})
	utcIdx := data.columnIndexByName(utcCol)
	dateIdx := data.columnIndexByName(dateCol)
	timeIdx := data.columnIndexByName(timeCol)
	return buildTimeSelectorSeriesNative(data, utcIdx, dateIdx, timeIdx)
}

func buildTimeSelectorSeriesNative(data *CSVData, utcIdx, dateIdx, timeIdx int) (timeSeriesNative, string) {
	dateTimeSeries, dateTimeStrategy := buildTimeMillisSeriesNative(data, -1, dateIdx, timeIdx)
	if dateTimeStrategy == "missing" {
		return buildTimeMillisSeriesNative(data, utcIdx, -1, -1)
	}

	utcSeries, utcStrategy := buildTimeMillisSeriesNative(data, utcIdx, -1, -1)
	if utcStrategy == "missing" {
		return dateTimeSeries, dateTimeStrategy
	}

	usedFallback := false
	for i := range dateTimeSeries.Values {
		if dateTimeSeries.Valid[i] {
			continue
		}
		if i < len(utcSeries.Valid) && utcSeries.Valid[i] {
			dateTimeSeries.Values[i] = utcSeries.Values[i]
			dateTimeSeries.Valid[i] = true
			usedFallback = true
		}
	}
	if usedFallback {
		return dateTimeSeries, "date_time_with_utc_fallback"
	}
	return dateTimeSeries, dateTimeStrategy
}

func excludeRowsByTimeWindows(data *CSVData, windows []TimeWindow) (*CSVData, int, error) {
	if data == nil {
		return nil, 0, fmt.Errorf("nil CSVData")
	}
	intervals, err := parseConfiguredTimeWindows(windows)
	if err != nil {
		return nil, 0, err
	}
	if len(intervals) == 0 {
		return data.clone(), 0, nil
	}

	series, strategy := timeSeriesForSorting(data)
	if strategy == "missing" {
		return nil, 0, fmt.Errorf("v súbore sa nenašli použiteľné časové údaje (UTC alebo Date + Time)")
	}

	out := &CSVData{
		Columns:        append([]string(nil), data.Columns...),
		Rows:           make([][]string, 0, len(data.Rows)),
		FileInfo:       data.FileInfo,
		InputRadioTech: data.InputRadioTech,
	}
	removed := 0

	for i, row := range data.Rows {
		if i < len(series.Valid) && series.Valid[i] && timeInAnyWindow(series.Values[i], intervals) {
			removed++
			continue
		}
		out.Rows = append(out.Rows, append([]string(nil), row...))
	}

	return out, removed, nil
}

type parsedTimeWindow struct {
	startMS int64
	endMS   int64
}

func parseConfiguredTimeWindows(windows []TimeWindow) ([]parsedTimeWindow, error) {
	parsed := make([]parsedTimeWindow, 0, len(windows))
	for _, window := range windows {
		start := strings.TrimSpace(window.Start)
		end := strings.TrimSpace(window.End)
		if start == "" || end == "" {
			continue
		}
		startMS, ok := parseDateTimeToMillis(start)
		if !ok {
			return nil, fmt.Errorf("neplatný začiatok časového úseku: %q", start)
		}
		endMS, ok := parseDateTimeToMillis(end)
		if !ok {
			return nil, fmt.Errorf("neplatný koniec časového úseku: %q", end)
		}
		if endMS < startMS {
			return nil, fmt.Errorf("koniec časového úseku musí byť po začiatku")
		}
		parsed = append(parsed, parsedTimeWindow{startMS: startMS, endMS: endMS})
	}
	return parsed, nil
}

func timeInAnyWindow(timestampMS int64, windows []parsedTimeWindow) bool {
	for _, window := range windows {
		if timestampMS >= window.startMS && timestampMS <= window.endMS {
			return true
		}
	}
	return false
}
