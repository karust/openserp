package core

import (
	api2captcha "github.com/2captcha/2captcha-go"
)

type CaptchaSolver struct {
	client *api2captcha.Client
}

func NewSolver(apikey string) *CaptchaSolver {
	cs := CaptchaSolver{}
	cs.client = api2captcha.NewClient(apikey)
	return &cs
}

func (cs *CaptchaSolver) SolveReCaptcha2(sitekey, pageUrl, dataS string) (string, error) {
	cap := api2captcha.ReCaptcha{
		SiteKey:   sitekey,
		Url:       pageUrl,
		DataS:     dataS,
		Invisible: false,
		Action:    "verify",
	}
	req := cap.ToRequest()
	req.SetProxy("HTTPS", "login:password@IP_address:PORT")
	return cs.client.Solve(req)
}
