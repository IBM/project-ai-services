package spinner

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/yarlson/pin"
)

type Spinner struct {
	p      *pin.Pin
	ctx    context.Context
	cancel context.CancelFunc
}

func New(message string) *Spinner {
	p := pin.New(message,
		pin.WithDoneSymbol('✔'),
		pin.WithDoneSymbolColor(pin.ColorGreen),
		pin.WithFailSymbol('✖'),
		pin.WithFailSymbolColor(pin.ColorRed),
	)

	return &Spinner{
		p: p,
	}
}

func (s *Spinner) Start(ctx context.Context) {
	s.ctx = ctx
	s.cancel = s.p.Start(ctx)
}

func (s *Spinner) Stop(message string) {
	if s.cancel != nil {
		s.cancel()
	}
	s.p.Stop(message)
}

func (s *Spinner) Fail(message string) {
	if s.cancel != nil {
		s.cancel()
	}
	s.p.Fail(message)
}

func (s *Spinner) UpdateMessage(message string) {
	s.p.UpdateMessage(message)
}

func (s *Spinner) StopWithHint(msg, hint string) {
	s.Fail(msg)
	logger.Infof("HINT: %s\n", hint)
}
