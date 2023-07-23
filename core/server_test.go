package core

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

var (
	servHost = "127.0.0.1"
	servPort = 7070
	servAddr = fmt.Sprintf("%s:%d", servHost, servPort)
)

type SeMock struct {
	EngineName string
}

func (s SeMock) Name() string {
	return s.EngineName
}
func (SeMock) IsInitialized() bool {
	return true
}
func (s SeMock) Search(q Query) (res []SearchResult, err error) {
	return []SearchResult{{Title: s.EngineName}}, nil
}
func (s SeMock) SearchImage(q Query) (res []SearchResult, err error) {
	return []SearchResult{{Title: s.EngineName}}, nil
}
func (s SeMock) GetRateLimiter() *rate.Limiter {
	return nil
}

func TestCreateServer(t *testing.T) {
	se1 := SeMock{"mock_engine_1"}
	se2 := SeMock{"mock_engine_2"}

	server := NewServer(servHost, servPort, se1, se2)

	go func() {
		time.Sleep(1 * time.Second)
		server.Shutdown()
	}()

	err := server.Listen()
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}
}

// func TestServerSearch(t *testing.T) {
// 	se := SeMock{"mock_engine"}
// 	server := NewServer("127.0.0.1", 7070, se)

// 	go func() {
// 		server.Listen()
// 	}()

// }
