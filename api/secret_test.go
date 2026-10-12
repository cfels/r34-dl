package api

import (
	"errors"
	"io"
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

type credentialRedirectTransport struct{ hits int }

func (r *credentialRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hits++
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": {"https://evil.example/?" + req.URL.RawQuery}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func TestRule34RejectsCredentialRedirect(t *testing.T) {
	client := NewRule34Client("777", "secret-value")
	transport := &credentialRedirectTransport{}
	client.http.Transport = transport
	err := client.Ping()
	if err == nil || transport.hits != 1 {
		t.Fatalf("credential redirect was followed: hits=%d, err=%v", transport.hits, err)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatal("redirect error leaked credentials")
	}
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
