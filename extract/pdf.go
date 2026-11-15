package extract

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MuPDF requires seekable input. Decode only the bytes fetched by the caller.
func extractPDF(ctx context.Context, req ExtractRequest, resp *FetchResponse, started time.Time) (*ExtractResult, error) {
	if len(resp.Body) > req.MaxBytes {
		return nil, errors.New("PDF exceeds extraction byte limit")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	file, err := os.CreateTemp("", "openserp-*.pdf")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(resp.Body)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	cmd := exec.CommandContext(ctx, "mutool", "draw", "-F", "txt", "-o", "-", file.Name())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, errors.New("PDF extraction requires mutool")
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
	markdown := pdfMarkdown(string(data))
	return &ExtractResult{URL: req.URL, Canonical: firstNonEmpty(resp.FinalURL, req.URL), Text: text, Markdown: markdown, Meta: ExtractMeta{ModeUsed: string(ModeFast), Bytes: len(resp.Body), FetchedAt: time.Now().UTC().Format(time.RFC3339), TookMs: time.Since(started).Milliseconds()}}, nil
}

// Widely spaced prose may arrive as a separate block for each visual line.
// Join continuations within a page while keeping headings and tables separate.
func pdfMarkdown(text string) string {
	var markdown strings.Builder
	for _, page := range strings.Split(text, "\f") {
		joinNext := false
		for _, block := range strings.Split(collapseBlankLines(page), "\n\n") {
			fields := strings.Fields(block)
			if len(fields) == 0 {
				continue
			}
			line := strings.Join(fields, " ")
			first, _ := utf8.DecodeRuneInString(line)
			if joinNext && unicode.IsLower(first) {
				markdown.WriteByte(' ')
			} else if markdown.Len() > 0 {
				markdown.WriteString("\n\n")
			}
			markdown.WriteString(line)
			last, _ := utf8.DecodeLastRuneInString(line)
			joinNext = len(fields) >= 6 && (unicode.IsLetter(last) || last == ',')
		}
	}
	return markdown.String()
}
