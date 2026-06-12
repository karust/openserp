package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestValidatePublicHTTPURLRejectsPrivateTargets(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://10.0.0.1/",
		"http://172.16.0.1/",
		"http://192.168.1.1/",
		"http://169.254.169.254/",
		"http://100.64.0.1/",
	}

	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			err := ValidatePublicHTTPURL(context.Background(), rawURL)
			if !errors.Is(err, ErrTargetNotAllowed) {
				t.Fatalf("expected ErrTargetNotAllowed, got %v", err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "non-public") {
				t.Fatalf("expected non-public error, got %v", err)
			}
		})
	}
}

func TestValidatePublicHTTPURLRejectsUnsupportedScheme(t *testing.T) {
	err := ValidatePublicHTTPURL(context.Background(), "file:///etc/passwd")
	if !errors.Is(err, ErrTargetNotAllowed) {
		t.Fatalf("expected ErrTargetNotAllowed, got %v", err)
	}
	if !strings.Contains(err.Error(), "only http and https") {
		t.Fatalf("unexpected error: %v", err)
	}
}
