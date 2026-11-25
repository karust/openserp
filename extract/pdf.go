package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Sniff the body, servers often label PDFs as octet-stream.
func isPDF(body []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(body, " \t\r\n"), []byte("%PDF-"))
}

func extractPDF(ctx context.Context, req ExtractRequest, resp *FetchResponse, startedAt time.Time) (*ExtractResult, error) {
	if err := classifyStatus(resp.StatusCode); err != nil {
		return nil, err
	}
	// A cut PDF loses its trailer, MuPDF would fail or return a fragment.
	if resp.Truncated || len(resp.Body) > req.MaxBytes {
		return nil, fmt.Errorf("PDF exceeds the %d-byte extraction limit", req.MaxBytes)
	}
	raw, err := pdfText(ctx, resp.Body, req.MaxBytes)
	if err != nil {
		return nil, err
	}
	text := normalizeMarkdown(strings.ReplaceAll(raw, "\f", "\n\n"))
	if text == "" {
		return nil, errors.New("PDF has no extractable text")
	}
	return &ExtractResult{
		URL:       req.URL,
		Canonical: firstNonEmpty(resp.FinalURL, req.URL),
		Text:      text,
		Markdown:  pdfMarkdown(raw),
		Meta: ExtractMeta{
			ModeUsed:  string(ModeFast),
			FetchedAt: time.Now().UTC().Format(time.RFC3339),
			Bytes:     len(resp.Body),
			TookMs:    time.Since(startedAt).Milliseconds(),
		},
	}, nil
}

// pdfText returns `mutool draw -F txt` output: blank line after each block, \f after each page.
// MuPDF needs seekable input, so the body goes through a temp file.
func pdfText(ctx context.Context, body []byte, limit int) (string, error) {
	file, err := os.CreateTemp("", "openserp-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mutool", "draw", "-F", "txt", "-o", "-", file.Name())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("PDF extraction requires mutool (mupdf-tools)")
		}
		return "", fmt.Errorf("start mutool: %w", err)
	}
	out, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	if readErr != nil || len(out) > limit {
		// mutool may be blocked on a full pipe, kill it so Wait returns.
		cancel()
	}
	waitErr := cmd.Wait()
	if len(out) > limit {
		return "", errors.New("PDF text exceeds extraction byte limit")
	}
	if readErr != nil || waitErr != nil {
		return "", errors.New("unable to extract PDF text")
	}
	return string(out), nil
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
