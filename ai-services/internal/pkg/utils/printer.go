package utils

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type Printer struct {
	model table.Model
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

	return &Printer{model: t}
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

func collapseFirstColumn(rows []table.Row) []table.Row {
	if len(rows) == 0 {
		return rows
	}

	last := ""
	for i, r := range rows {
		if len(r) == 0 {
			continue
		}

		if r[0] == last {
			r[0] = "" // blank repeated values
		} else {
			last = r[0]
		}

		rows[i] = r
	}

	return rows
}

func (p *Printer) CloseTableWriter() {
	cols := p.model.Columns()
	rows := collapseFirstColumn(p.model.Rows())

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

		// truncate at 30 chars max
		if maxLen+2 <= 30 {
			cols[colIdx].Width = maxLen + 2
		} else {
			cols[colIdx].Width = 30
		}
	}

	p.model.SetColumns(cols)

	out := p.model.View()

	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			logger.Infoln(line)
		}
	}

	p.model.SetRows([]table.Row{})
}
