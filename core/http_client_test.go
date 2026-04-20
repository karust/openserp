package core

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

func TestDrainAndCloseResponseDrainsAndCloses(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("unread payload")}
	resp := &http.Response{Body: body}

	DrainAndCloseResponse(resp)

	if !body.closed {
		t.Fatal("expected body to be closed")
	}

	n, err := body.Read(make([]byte, 1))
	if err != io.EOF || n != 0 {
		t.Fatalf("expected body drained to EOF, got n=%d err=%v", n, err)
	}
}

func TestDrainAndCloseResponseNilSafe(t *testing.T) {
	DrainAndCloseResponse(nil)
	DrainAndCloseResponse(&http.Response{})
}
