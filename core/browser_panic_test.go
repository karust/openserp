package core

import (
	"errors"
	"testing"
)

func TestRecoverEnginePanic_ReturnsTypedError(t *testing.T) {
	err := RecoverEnginePanic("test-engine", "boom", nil)
	if !errors.Is(err, ErrEngineInternal) {
		t.Fatalf("expected ErrEngineInternal, got: %v", err)
	}
}
