package api

import (
	"encoding/csv"
	"net/http"
	"strings"
)

// formulaStarts are the characters that make a spreadsheet read a cell as
// a formula, or (tab, carriage return) can sit in front of one that does.
const formulaStarts = "=+-@\t\r"

// spreadsheetSafe keeps a cell from running as a formula when the file is
// opened in a spreadsheet: monitor names, targets, causes and notes are
// written by users and by the monitored services, and "=HYPERLINK(…)" in
// one of them must stay text. A leading apostrophe makes it text; the
// spreadsheet shows the cell without it.
func spreadsheetSafe(cell string) string {
	if cell != "" && strings.ContainsRune(formulaStarts, rune(cell[0])) {
		return "'" + cell
	}
	return cell
}

// csvDownload writes a CSV attachment whose cells are all spreadsheetSafe.
type csvDownload struct {
	w *csv.Writer
}

// newCSVDownload answers with a CSV file of the given name.
func newCSVDownload(w http.ResponseWriter, filename string) *csvDownload {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return &csvDownload{w: csv.NewWriter(w)}
}

// row writes one row; cells is changed in place.
func (d *csvDownload) row(cells ...string) {
	for i, cell := range cells {
		cells[i] = spreadsheetSafe(cell)
	}
	_ = d.w.Write(cells)
}

func (d *csvDownload) flush() { d.w.Flush() }
