package backend

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

const csvPreviewMaxBytes = 1 << 20

// LoadCSVSchema reads a bounded prefix, never a full measurement dataset.
// Extra columns are inferred from this sample; processing still checks all rows.
func LoadCSVSchema(path string) (*CSVData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("vstup CSV nie je bežný súbor")
	}
	return readCSVSchema(f)
}

func readCSVSchema(reader io.Reader) (*CSVData, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, csvPreviewMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > csvPreviewMaxBytes {
		// Do not parse an incomplete row (or a split UTF-8 character).
		end := bytes.LastIndexByte(raw[:csvPreviewMaxBytes], '\n')
		if end < 0 {
			return nil, fmt.Errorf("hlavička CSV presahuje limit náhľadu 1 MiB")
		}
		raw = raw[:end+1]
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("CSV súbor je prázdny")
	}
	data, err := parseCSVBytes(raw)
	if err != nil {
		return nil, err
	}
	data.Rows = nil
	// Headers are substrings of the sample; do not retain the whole prefix in
	// the application's per-file metadata cache.
	for i, col := range data.Columns {
		data.Columns[i] = strings.Clone(col)
	}
	data.FileInfo.OriginalHeader = strings.Clone(data.FileInfo.OriginalHeader)
	return data, nil
}

// MergeCSVSchema combines column names only. It never loads or copies rows.
func MergeCSVSchema(schemas []*CSVData) (*CSVData, error) {
	if len(schemas) == 0 {
		return nil, fmt.Errorf("žiadna CSV schéma")
	}
	columns, err := buildUnionColumns(schemas, nil, nil)
	if err != nil {
		return nil, err
	}
	info := schemas[0].FileInfo
	if len(schemas) > 1 {
		info.OriginalHeader = strings.Join(columns, ";")
	}
	return &CSVData{Columns: columns, FileInfo: info, InputRadioTech: DetectInputRadioTech(columns)}, nil
}
