package erp

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var ErrSecurityAnswerNotRequested = errors.New("no ERP security answer is currently requested")

type securityAnswerResult struct {
	value string
	err   error
}

// InteractiveSecurityAnswer bridges an unknown ERP security question and the
// protected local browser UI without exposing any previously saved answers.
type InteractiveSecurityAnswer struct {
	mu       sync.Mutex
	question string
	waiting  chan securityAnswerResult
	closed   bool
}

func NewInteractiveSecurityAnswer() *InteractiveSecurityAnswer {
	return &InteractiveSecurityAnswer{}
}

func (p *InteractiveSecurityAnswer) Wait(ctx context.Context, question string) (string, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return "", errors.New("interactive security answer provider is closed")
	}
	if p.waiting != nil {
		p.mu.Unlock()
		return "", errors.New("ERP security answer is already being awaited")
	}
	results := make(chan securityAnswerResult, 1)
	p.question = strings.TrimSpace(question)
	p.waiting = results
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.waiting == results {
			p.waiting = nil
			p.question = ""
		}
		p.mu.Unlock()
	}()

	select {
	case result := <-results:
		return result.value, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (p *InteractiveSecurityAnswer) Submit(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("security answer is required")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("interactive security answer provider is closed")
	}
	if p.waiting == nil {
		return ErrSecurityAnswerNotRequested
	}
	select {
	case p.waiting <- securityAnswerResult{value: value}:
		return nil
	default:
		return errors.New("an ERP security answer has already been submitted")
	}
}

func (p *InteractiveSecurityAnswer) Current() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.question, p.waiting != nil
}

func (p *InteractiveSecurityAnswer) Close() {
	p.mu.Lock()
	p.closed = true
	if p.waiting != nil {
		select {
		case p.waiting <- securityAnswerResult{err: errors.New("interactive security answer provider closed")}:
		default:
		}
	}
	p.mu.Unlock()
}
