package erp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInteractiveOTPSubmitAndTimeout(t *testing.T) {
	provider := NewInteractiveOTP()
	result := make(chan string, 1)
	go func() { value, _ := provider.Wait(context.Background()); result <- value }()
	deadline := time.Now().Add(time.Second)
	for !provider.Waiting() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := provider.Submit("123456"); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != "123456" {
		t.Fatalf("got %q", got)
	}
	if err := provider.Submit("123456"); !errors.Is(err, ErrOTPNotRequested) {
		t.Fatalf("got %v", err)
	}
}

func TestInteractiveOTPRejectsMalformedInput(t *testing.T) {
	provider := NewInteractiveOTP()
	if err := provider.Submit("123"); err == nil {
		t.Fatal("short OTP accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
