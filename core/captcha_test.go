package core

import (
	"testing"
)

var (
	API_KEY = ""
)

func Test2Captcha(t *testing.T) {
	solver := NewSolver(API_KEY)
	sitekey := "6LfwuyUTAAAAAOAmoS0fdqijC2PbbdH4kjq62Y1b"
	url := "https://www.google.com/sorry/index?continue=https://www.google.de/search%3Fhl%3DDE%26lr%3Dlang_de%26nfpr%3D1%26num%3D500%26pws%3D0%26q%3Dwhere%2Bwhy%2Beach&hl=DE&q=EgRegw55GObHiq4GIjDqmzFKayGXrS2-s9ooWfcskhpK8-6tIjWSaSvhxd3f5eAyUXj7lYq2DYLDXB8ASz0yAXJaAUM"
	datas := "Ghk0n7ZQNDS0c7ES53eef_YBfSdfeXnyRD0p2OR0R4Dg91CUXKS_hio5Do6TpJ8sHhhOat_NymTASZGe1gqAjP7w9dSvhvRT7QXsrdziO3JPngLDSRzDdjT42GDcSbO0kzInlDPxe1yy2t4yifo9xHpMnlZU7pTVNTQUIXqOMLHAR-iERi6aoSQDQ4d-88-jW3LEinquxEut0OhHG2l2stwG9AnCmNvCsUNJda-H24saFlOh5csK9KNXeeQmpr6at52_skMIMiLXSlY56vYFVCRMkXLQdAM"
	resp, err := solver.SolveReCaptcha2(sitekey, url, datas)
	if err != nil || resp == "" {
		t.Fatalf("Failed to solve recaptchaV2: %s", err)
	}
}
