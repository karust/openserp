package cmd

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

func resolveCaptchaSolverConfig() (bool, string, error) {
	apiKey := strings.TrimSpace(config.Config2Capcha.ApiKey)
	if !config.Captcha.SolverEnabled {
		if apiKey != "" {
			logrus.Warn("2captcha.apikey is set but captcha.solver_enabled=false; solver will not run")
		}
		return false, "", nil
	}

	if apiKey == "" {
		return false, "", fmt.Errorf("captcha solver is enabled (captcha.solver_enabled=true) but 2captcha.apikey is empty")
	}

	return true, apiKey, nil
}
