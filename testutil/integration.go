package testutil

import (
	"os"
	"strings"
	"testing"
)

const IntegrationEnv = "OPENSERP_INTEGRATION_TESTS"

func RequireIntegration(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(IntegrationEnv)) != "1" {
		t.Skipf("set %s=1 to run integration tests", IntegrationEnv)
	}
}

func RequireEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("set %s to run this integration test", key)
	}
	return value
}
