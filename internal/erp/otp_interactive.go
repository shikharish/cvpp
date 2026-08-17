package erp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrOTPNotRequested = errors.New("no ERP OTP is currently requested")
	ErrOTPAlreadySent  = errors.New("an ERP OTP has already been submitted")
)

type otpResult struct {
	value string
	err   error
}

// InteractiveOTP bridges the ERP OTPProvider contract and the browser. Wait
// blocks only the ERP worker goroutine; Submit is called by the protected local
// API and never exposes the OTP in an HTTP response or log.
type InteractiveOTP struct {
	mu      sync.Mutex
	waiting chan otpResult
	closed  bool
}

func NewInteractiveOTP() *InteractiveOTP { return &InteractiveOTP{} }

func (p *InteractiveOTP) Prepare(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("interactive OTP provider is closed")
	}
	return nil
}

func (p *InteractiveOTP) Wait(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return "", errors.New("interactive OTP provider is closed")
	}
	if p.waiting != nil {
		p.mu.Unlock()
		return "", errors.New("ERP OTP is already being awaited")
	}
	results := make(chan otpResult, 1)
	p.waiting = results
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.waiting == results {
			p.waiting = nil
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

func (p *InteractiveOTP) Submit(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 4 || len(value) > 8 || strings.Trim(value, "0123456789") != "" {
		return fmt.Errorf("OTP must contain 4 to 8 digits")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("interactive OTP provider is closed")
	}
	if p.waiting == nil {
		return ErrOTPNotRequested
	}
	select {
	case p.waiting <- otpResult{value: value}:
		return nil
	default:
		return ErrOTPAlreadySent
	}
}

func (p *InteractiveOTP) Cancel(err error) {
	if err == nil {
		err = context.Canceled
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waiting != nil {
		select {
		case p.waiting <- otpResult{err: err}:
		default:
		}
	}
}

func (p *InteractiveOTP) Close() {
	p.mu.Lock()
	p.closed = true
	if p.waiting != nil {
		select {
		case p.waiting <- otpResult{err: errors.New("interactive OTP provider closed")}:
		default:
		}
	}
	p.mu.Unlock()
}

func (p *InteractiveOTP) Waiting() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waiting != nil
}
