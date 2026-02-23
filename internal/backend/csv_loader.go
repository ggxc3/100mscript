package backend

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

type CSVFileInfo struct {
	Encoding       string
	HeaderLine     int
	OriginalHeader string
}

type CSVData struct {
	Columns  []string
	Rows     [][]string
	FileInfo CSVFileInfo
}

type textDecoder struct {
	name   string
	decode func([]byte) (string, error)
}

func splitSemicolonColumns(line string) []string {
	trimmed := strings.TrimRight(line, "\r\n")
	cols := strings.Split(trimmed, ";")
	for len(cols) > 0 && cols[len(cols)-1] == "" {
		cols = cols[:len(cols)-1]
	}
	return cols
}

func hasTabularFollowup(lines []string, startIndex, expectedColumns, minColumns int) bool {
	seenCandidates := 0
	tabularRows := 0
	threshold := maxInt(minColumns, expectedColumns-1)

	for i := startIndex + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		seenCandidates++
		colsCount := len(splitSemicolonColumns(line))
		if colsCount >= threshold {
			tabularRows++
			if tabularRows >= 2 {
				return true
			}
		}
		if seenCandidates >= 25 {
			break
		}
	}
	return false
}

func findTabularHeader(lines []string, minColumns int) (int, string) {
	firstCandidateIndex := -1
	firstCandidateLine := ""

	for i, line := range lines {
		cols := splitSemicolonColumns(line)
		if len(cols) < minColumns {
			continue
		}
		if firstCandidateIndex == -1 {
			firstCandidateIndex = i
			firstCandidateLine = strings.TrimSpace(line)
		}
		if hasTabularFollowup(lines, i, len(cols), minColumns) {
			return i, strings.TrimSpace(line)
		}
	}

	if firstCandidateIndex != -1 {
		return firstCandidateIndex, firstCandidateLine
	}
	return -1, ""
}

func makeUniqueColumnNames(columns []string) []string {
	result := make([]string, 0, len(columns))
	seen := map[string]int{}
	for i, raw := range columns {
		base := strings.TrimSpace(raw)
		if base == "" {
			base = fmt.Sprintf("column_%d", i+1)
		}
		seen[base]++
		if seen[base] == 1 {
			result = append(result, base)
		} else {
			result = append(result, fmt.Sprintf("%s_%d", base, seen[base]))
		}
	}
	return result
}

func decodeUTF8(data []byte) (string, error) {
	if !utf8.Valid(data) {
		return "", fmt.Errorf("invalid utf-8")
	}
	return string(data), nil
}

func defaultTextDecoders() []textDecoder {
	return []textDecoder{
		{name: "utf-8", decode: decodeUTF8},
		{name: "latin1", decode: func(b []byte) (string, error) { return charmap.ISO8859_1.NewDecoder().String(string(b)) }},
		{name: "latin2", decode: func(b []byte) (string, error) { return charmap.ISO8859_2.NewDecoder().String(string(b)) }},
		{name: "cp1250", decode: func(b []byte) (string, error) { return charmap.Windows1250.NewDecoder().String(string(b)) }},
		{name: "windows-1250", decode: func(b []byte) (string, error) { return charmap.Windows1250.NewDecoder().String(string(b)) }},
		{name: "iso-8859-2", decode: func(b []byte) (string, error) { return charmap.ISO8859_2.NewDecoder().String(string(b)) }},
	}
}

func splitLinesPreserveEmpty(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Split(bufio.ScanLines)
	lines := make([]string, 0, 1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	// bufio.ScanLines drops final empty line information which we do not need here.
	return lines
}

func decodeWithCandidates(data []byte) (string, string, []string, int, string, error) {
	headerLine := -1
	var originalHeader string
	var selectedText string
	var selectedEncoding string

	for _, dec := range defaultTextDecoders() {
		text, err := dec.decode(data)
		if err != nil {
			continue
		}
		lines := splitLinesPreserveEmpty(text)
		foundHeaderLine, foundHeader := findTabularHeader(lines, 6)
		if foundHeaderLine != -1 {
			return text, dec.name, lines, foundHeaderLine, foundHeader, nil
		}
		// fallback preference if file is decodable but header not found yet
		if selectedText == "" {
			selectedText = text
			selectedEncoding = dec.name
			headerLine = foundHeaderLine
			originalHeader = foundHeader
		}
	}

	if selectedText == "" {
		return "", "", nil, -1, "", fmt.Errorf("unable to decode CSV with supported encodings")
	}

	lines := splitLinesPreserveEmpty(selectedText)
	if headerLine == -1 {
		headerLine, originalHeader = findTabularHeader(lines, 6)
	}
	return selectedText, selectedEncoding, lines, headerLine, originalHeader, nil
}

func LoadCSVFile(path string) (*CSVData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	text, encodingName, lines, headerLine, originalHeader, err := decodeWithCandidates(raw)
	if err != nil {
		return nil, err
	}
	if headerLine == -1 {
		headerLine = 0
	}

	headerCols := []string{}
	if originalHeader != "" {
		headerCols = splitSemicolonColumns(originalHeader)
	}
	if len(headerCols) == 0 && headerLine >= 0 && headerLine < len(lines) {
		headerCols = splitSemicolonColumns(lines[headerLine])
	}

	maxFields := len(headerCols)
	for lineNo, line := range lines {
		if lineNo <= headerLine || strings.TrimSpace(line) == "" {
			continue
		}
		maxFields = maxInt(maxFields, len(splitSemicolonColumns(line)))
	}
	if maxFields <= 0 {
		maxFields = maxInt(len(headerCols), 1)
	}
	if len(headerCols) > maxFields {
		headerCols = headerCols[:maxFields]
	}

	// If header was shorter than data, append deterministic extra columns.
	if len(headerCols) < maxFields {
		missingStart := len(headerCols)
		for i := missingStart; i < maxFields; i++ {
			headerCols = append(headerCols, fmt.Sprintf("extra_col_%d", i-missingStart+1))
		}
	}
	if len(headerCols) != maxFields {
		// Rebuild deterministically when prior loops were confusing due to header length mutation.
		fixed := make([]string, 0, maxFields)
		for i := 0; i < maxFields; i++ {
			if i < len(headerCols) {
				fixed = append(fixed, headerCols[i])
			} else {
				fixed = append(fixed, fmt.Sprintf("extra_col_%d", i-len(headerCols)+1))
			}
		}
		headerCols = fixed
	}

	columnNames := makeUniqueColumnNames(headerCols)

	rows := make([][]string, 0, maxInt(0, len(lines)-headerLine-1))
	for lineNo, line := range lines {
		if lineNo <= headerLine || strings.TrimSpace(line) == "" {
			continue
		}
		fields := splitSemicolonColumns(line)
		if len(fields) < maxFields {
			padded := make([]string, maxFields)
			copy(padded, fields)
			fields = padded
		} else if len(fields) > maxFields {
			fields = fields[:maxFields]
		}
		rows = append(rows, fields)
	}

	// Normalize empty original header if file had CRLF only artifacts.
	originalHeader = strings.TrimRight(originalHeader, "\r\n")
	if originalHeader == "" && headerLine >= 0 && headerLine < len(lines) {
		originalHeader = strings.TrimRight(lines[headerLine], "\r\n")
	}

	_ = text // retained for future use/debug parity; parser currently uses split lines.
	return &CSVData{
		Columns: columnNames,
		Rows:    rows,
		FileInfo: CSVFileInfo{
			Encoding:       encodingName,
			HeaderLine:     headerLine,
			OriginalHeader: originalHeader,
		},
	}, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
