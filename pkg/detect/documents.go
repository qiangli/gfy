package detect

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/nguyenthenguyen/docx"
	"github.com/xuri/excelize/v2"
)

// ExtractPDFText extracts plain text from a PDF file.
func ExtractPDFText(path string) string {
	f, r, err := pdf.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err == nil {
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// DocxToMarkdown converts a .docx file to markdown text.
func DocxToMarkdown(path string) string {
	r, err := docx.ReadDocxFile(path)
	if err != nil {
		return ""
	}
	defer r.Close()

	doc := r.Editable()
	return doc.GetContent()
}

// XlsxToMarkdown converts an .xlsx file to markdown tables.
func XlsxToMarkdown(path string) string {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var b strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil || len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## Sheet: %s\n", sheet)
		// Header
		b.WriteString("| " + strings.Join(rows[0], " | ") + " |\n")
		b.WriteString("| " + strings.Repeat("--- | ", len(rows[0])) + "\n")
		for _, row := range rows[1:] {
			// Pad to header length.
			for len(row) < len(rows[0]) {
				row = append(row, "")
			}
			b.WriteString("| " + strings.Join(row, " | ") + " |\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
