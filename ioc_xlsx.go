package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

const (
	iocXLSXColValue = "值（IP/网段）"
	iocXLSXColLabel = "备注"
)

func xmlEscapeText(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

func buildIOCTemplateXLSX() ([]byte, error) {
	rows := [][2]string{
		{iocXLSXColValue, iocXLSXColLabel},
		{"1.2.3.4", "已知扫描器"},
		{"10.0.0.0/8", "内部保留"},
	}

	var sheetRows strings.Builder
	for i, row := range rows {
		fmt.Fprintf(&sheetRows, `<row r="%d">`, i+1)
		for j, cell := range row {
			ref := string(rune('A'+j)) + strconv.Itoa(i+1)
			fmt.Fprintf(&sheetRows, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, xmlEscapeText(cell))
		}
		sheetRows.WriteString("</row>")
	}

	sheetXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + sheetRows.String() + `</sheetData></worksheet>`

	const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`

	const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`

	const workbookXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="IOC" sheetId="1" r:id="rId1"/></sheets></workbook>`

	const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := []struct {
		name    string
		content string
	}{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"xl/workbook.xml", workbookXML},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/worksheets/sheet1.xml", sheetXML},
	}
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(f.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type xlsxSST struct {
	Items []xlsxSSTItem `xml:"si"`
}

type xlsxSSTItem struct {
	T    string       `xml:"t"`
	Runs []xlsxSSTRun `xml:"r"`
}

type xlsxSSTRun struct {
	T string `xml:"t"`
}

func (it xlsxSSTItem) text() string {
	if it.T != "" {
		return it.T
	}
	var b strings.Builder
	for _, r := range it.Runs {
		b.WriteString(r.T)
	}
	return b.String()
}

type xlsxSheetXML struct {
	Rows []xlsxRowXML `xml:"sheetData>row"`
}

type xlsxRowXML struct {
	Cells []xlsxCellXML `xml:"c"`
}

type xlsxCellXML struct {
	Ref    string         `xml:"r,attr"`
	Type   string         `xml:"t,attr"`
	Value  string         `xml:"v"`
	Inline *xlsxInlineStr `xml:"is"`
}

type xlsxInlineStr struct {
	T string `xml:"t"`
}

func cellColumnIndex(ref string) int {
	col := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		col = col*26 + int(r-'A'+1)
	}
	return col - 1
}

func readZipFile(zr *zip.Reader, name string) ([]byte, bool) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, false
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, false
			}
			return data, true
		}
	}
	return nil, false
}

func parseIOCXLSX(r io.ReaderAt, size int64) ([]IOCEntryRecord, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}

	var sst []string
	if data, ok := readZipFile(zr, "xl/sharedStrings.xml"); ok {
		var parsed xlsxSST
		if err := xml.Unmarshal(data, &parsed); err != nil {
			return nil, fmt.Errorf("parse shared strings: %w", err)
		}
		for _, it := range parsed.Items {
			sst = append(sst, it.text())
		}
	}

	data, ok := readZipFile(zr, "xl/worksheets/sheet1.xml")
	if !ok {
		return nil, fmt.Errorf("xlsx has no worksheet")
	}
	var sheet xlsxSheetXML
	if err := xml.Unmarshal(data, &sheet); err != nil {
		return nil, fmt.Errorf("parse worksheet: %w", err)
	}

	var entries []IOCEntryRecord
	for i, row := range sheet.Rows {
		if i == 0 {
			continue
		}
		cols := map[int]string{}
		for _, c := range row.Cells {
			var text string
			switch c.Type {
			case "s":
				idx, err := strconv.Atoi(c.Value)
				if err == nil && idx >= 0 && idx < len(sst) {
					text = sst[idx]
				}
			case "inlineStr":
				if c.Inline != nil {
					text = c.Inline.T
				}
			default:
				text = c.Value
			}
			cols[cellColumnIndex(c.Ref)] = text
		}
		value := strings.TrimSpace(cols[0])
		if value == "" {
			continue
		}
		label := "IOC导入"
		if l := strings.TrimSpace(cols[1]); l != "" {
			label = l
		}
		kind := "ip"
		if strings.Contains(value, "/") {
			kind = "cidr"
		}
		if kind == "ip" {
			if _, err := ipToUint32(value); err != nil {
				return nil, fmt.Errorf("第 %d 行 IP 格式错误：%s", i+1, value)
			}
		} else if _, _, err := net.ParseCIDR(value); err != nil {
			return nil, fmt.Errorf("第 %d 行网段格式错误：%s", i+1, value)
		}
		entries = append(entries, IOCEntryRecord{Kind: kind, Value: value, Label: label})
	}
	return entries, nil
}
