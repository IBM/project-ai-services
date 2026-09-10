package utils

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	columnPadding = 2
)

// defaultCollapseIndices are the column indices collapsed by default (APPLICATION NAME, WORKER).
var defaultCollapseIndices = []int{0, 1}

type Printer struct {
	model           table.Model
	collapseIndices []int
}

func NewTableWriter() *Printer {
	t := table.New(
		table.WithColumns([]table.Column{}),
		table.WithRows([]table.Row{}),
		table.WithFocused(false),
	)

	styles := table.DefaultStyles()

	styles.Header = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Padding(0, 1).
		Bold(true)

	styles.Cell = lipgloss.NewStyle().
		Padding(0, 1)

	styles.Selected = lipgloss.NewStyle()

	t.SetStyles(styles)

	return &Printer{model: t, collapseIndices: defaultCollapseIndices}
}

// SetCollapseIndices overrides which column indices are collapsed when rendering.
func (p *Printer) SetCollapseIndices(indices ...int) {
	p.collapseIndices = indices
}

func (p *Printer) SetHeaders(headers ...string) {
	cols := make([]table.Column, len(headers))

	for i, h := range headers {
		cols[i] = table.Column{
			Title: h,
		}
	}

	p.model.SetColumns(cols)
}

func (p *Printer) AppendRow(cells ...string) {
	p.model.SetRows(append(p.model.Rows(), table.Row(cells)))
}

// collapseColumns blanks repeated values in the given column indices across rows.
// The first index is the anchor column (APPLICATION NAME): when it changes, all
// tracked columns re-print; when it stays the same, they are all blanked.
func collapseColumns(rows []table.Row, indices []int) []table.Row {
	if len(rows) == 0 || len(indices) == 0 {
		return rows
	}

	anchor := indices[0]
	last := make(map[int]string, len(indices))
	for i, r := range rows {
		if len(r) == 0 {
			continue
		}

		// Only the anchor column drives whether all tracked columns print or collapse.
		anchorChanged := anchor < len(r) && r[anchor] != last[anchor]

		for _, col := range indices {
			if col >= len(r) {
				continue
			}
			if anchorChanged {
				last[col] = r[col] // re-print and update baseline
			} else {
				r[col] = "" // blank repeated value
			}
		}

		rows[i] = r
	}

	return rows
}

func (p *Printer) CloseTableWriter() {
	cols := p.model.Columns()
	rows := collapseColumns(p.model.Rows(), p.collapseIndices)

	// Width of rows is computed here before rendering
	for colIdx := range cols {
		maxLen := len(cols[colIdx].Title)

		for _, row := range rows {
			if colIdx < len(row) {
				if l := len(row[colIdx]); l > maxLen {
					maxLen = l
				}
			}
		}

		cols[colIdx].Width = maxLen + columnPadding
	}

	p.model.SetColumns(cols)

	out := p.model.View()

	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) != "" {
			logger.Infoln(line)
		}
	}

	p.model.SetRows([]table.Row{})
}
