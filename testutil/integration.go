package testutil

import (
	"os"
	"strings"
	"testing"
)

const IntegrationEnv = "OPENSERP_INTEGRATION_TESTS"
const IntegrationHeadfulEnv = "OPENSERP_INTEGRATION_HEADFUL"
const IntegrationStrictEnv = "OPENSERP_INTEGRATION_STRICT"

func RequireIntegration(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(IntegrationEnv)) != "1" {
		t.Skipf("set %s=1 to run integration tests", IntegrationEnv)
	}
}

func IntegrationHeadful() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(IntegrationHeadfulEnv)))
	return value == "1" || value == "true" || value == "yes"
}

func IntegrationStrict() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(IntegrationStrictEnv)))
	return value == "1" || value == "true" || value == "yes"
}

func RequireEnv(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("set %s to run this integration test", key)
	}
	return value
}
