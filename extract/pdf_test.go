package extract

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestAutoExtractPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("install poppler-utils to exercise PDF extraction")
	}
	// A small, self-contained PDF with a real text layer and cross-reference table.
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	stream := "BT /F1 12 Tf 24 160 Td (Example reference text.) Tj ET"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"}
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	for _, tc := range []struct {
		body  string
		valid bool
	}{{b.String(), true}, {"%PDF-1.4 corrupt", false}} {
		e := Extractor{RawFetch: func(context.Context, ExtractRequest) (*FetchResponse, error) {
			return &FetchResponse{StatusCode: 200, Body: []byte(tc.body)}, nil
		}, RenderedFetch: func(context.Context, ExtractRequest) (*FetchResponse, error) {
			t.Fatal("PDF must not fall back to the browser")
			return nil, nil
		}}
		got, err := e.Extract(context.Background(), ExtractRequest{URL: "https://example.org/manual", Mode: ModeAuto})
		if tc.valid {
			if err != nil || got.Markdown != "Example reference text." {
				t.Fatalf("text extraction: %+v %v", got, err)
			}
		} else if err == nil || got != nil {
			t.Fatal("invalid PDF accepted")
		}
	}
}
