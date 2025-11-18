package helpers

import (
	"github.com/jedib0t/go-pretty/v6/table"
	"k8s.io/klog/v2"
)

type Printer struct {
	writer table.Writer
}

type klogWriter struct{}

func (k klogWriter) Write(p []byte) (int, error) {
	klog.Info(string(p))
	return len(p), nil
}

func NewTableWriter() *Printer {
	t := table.NewWriter()
	//Redirect output to klog
	t.SetOutputMirror(klogWriter{})
	t.SetStyle(table.StyleRounded)

	return &Printer{
		writer: t,
	}
}

func (p *Printer) GetTableWriter() table.Writer {
	return p.writer
}

func (p *Printer) CloseTableWriter() {
	p.writer.Render()
	p.writer.ResetHeaders()
	p.writer.ResetRows()
}
