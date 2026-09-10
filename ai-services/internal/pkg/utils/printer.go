package utils

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	columnPadding             = 2
	collapseLeadingColumnsNum = 2
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

func collapseLeadingColumns(rows []table.Row, n int) []table.Row {
	if len(rows) == 0 {
		return rows
	}

	last := make([]string, n)
	for i, r := range rows {
		if len(r) == 0 {
			continue
		}

		for col := 0; col < n && col < len(r); col++ {
			if r[col] == last[col] {
				r[col] = "" // blank repeated values
			} else {
				last[col] = r[col]
				// reset all subsequent tracked columns when a parent changes
				for j := col + 1; j < n; j++ {
					last[j] = ""
				}
			}
		}

		rows[i] = r
	}

	return rows
}

func (p *Printer) CloseTableWriter() {
	cols := p.model.Columns()
	rows := collapseLeadingColumns(p.model.Rows(), collapseLeadingColumnsNum)

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
