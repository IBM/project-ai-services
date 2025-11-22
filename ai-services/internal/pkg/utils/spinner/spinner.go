package spinner

import (
	"context"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type Spinner struct {
	prog *tea.Program
	done atomic.Bool
}

func New(msg string) *Spinner {
	m := newModel(msg)

	p := tea.NewProgram(
		m,
		tea.WithoutSignals(),
	)

	return &Spinner{prog: p}
}

func (s *Spinner) Start(ctx context.Context) {
	s.done.Store(false)

	go func() {
		_, _ = s.prog.Run()
	}()

	go func() {
		<-ctx.Done()
		if !s.done.Load() {
			s.prog.Send(stopMsg("cancelled"))
			s.done.Store(true)
		}
	}()
}

func (s *Spinner) Stop(msg string) {
	if s.done.Swap(true) {
		return
	}
	s.prog.Send(stopMsg(msg))
	time.Sleep(50 * time.Millisecond)
}

func (s *Spinner) Fail(msg string) {
	if s.done.Swap(true) {
		return
	}
	s.prog.Send(failMsg(msg))
	time.Sleep(50 * time.Millisecond)
}

func (s *Spinner) Update(msg string) {
	if !s.done.Load() {
		s.prog.Send(updateMsg(msg))
	}
}

func (s *Spinner) IsRunning() bool {
	return !s.done.Load()
}
