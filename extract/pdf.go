package extract

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Feed the already fetched bytes through stdin. Poppler never fetches the URL.
func extractPDF(ctx context.Context, req ExtractRequest, resp *FetchResponse, started time.Time) (*ExtractResult, error) {
	if len(resp.Body) > req.MaxBytes {
		return nil, errors.New("PDF exceeds extraction byte limit")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", "-eol", "unix", "-", "-")
	cmd.Stdin = bytes.NewReader(resp.Body)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, errors.New("PDF extraction requires pdftotext")
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, int64(req.MaxBytes)+1))
	if readErr != nil || len(data) > req.MaxBytes {
		cancel()
	}
	err = cmd.Wait()
	if len(data) > req.MaxBytes {
		return nil, errors.New("PDF text exceeds extraction byte limit")
	}
	if err != nil || readErr != nil {
		return nil, errors.New("unable to extract PDF text")
	}
	text := strings.TrimSpace(strings.ReplaceAll(string(data), "\f", "\n\n"))
	if text == "" {
		return nil, errors.New("PDF has no extractable text")
	}
	// PDF text wraps at visual line boundaries. Keep paragraph and page breaks.
	paragraphs := strings.Split(collapseBlankLines(text), "\n\n")
	for i, p := range paragraphs {
		paragraphs[i] = strings.Join(strings.Fields(p), " ")
	}
	markdown := strings.Join(paragraphs, "\n\n")
	return &ExtractResult{URL: req.URL, Canonical: firstNonEmpty(resp.FinalURL, req.URL), Text: text, Markdown: markdown, Meta: ExtractMeta{ModeUsed: string(ModeFast), Bytes: len(resp.Body), FetchedAt: time.Now().UTC().Format(time.RFC3339), TookMs: time.Since(started).Milliseconds()}}, nil
}
