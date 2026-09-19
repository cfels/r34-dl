package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

type failingTransport struct{ err error }

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if t.err == nil {
		return nil, errors.New("dial tcp: connection refused")
	}
	return nil, t.err
}

func TestRule34ErrorsDoNotLeakCredentials(t *testing.T) {
	const (
		userID = "777777"
		apiKey = "SUPERSECRETKEY123456"
	)
	client := NewRule34Client(userID, apiKey)
	client.http = &http.Client{Transport: failingTransport{}}

	calls := map[string]func() error{
		"search": func() error { _, err := client.SearchPosts("test", 1, 0); return err },
		"count":  func() error { _, err := client.CountPosts("test"); return err },
		"ping":   client.Ping,
	}
	for name, call := range calls {
		err := call()
		if err == nil {
			t.Fatalf("%s: expected a transport error", name)
		}
		if strings.Contains(err.Error(), apiKey) {
			t.Errorf("%s error leaked the api key: %v", name, err)
		}
		if strings.Contains(err.Error(), userID) {
			t.Errorf("%s error leaked the user id: %v", name, err)
		}
	}
}
