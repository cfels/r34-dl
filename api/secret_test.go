package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
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

func TestMediaCookiesCannotCrossPublicSuffixes(t *testing.T) {
	for _, tt := range []struct{ source, target, domain string }{
		{"https://attacker.com/", "https://www.pornhub.com/", "com"},
		{"https://attacker.co.uk/", "https://victim.co.uk/", "co.uk"},
	} {
		jar := mediaClient().Jar
		source, _ := url.Parse(tt.source)
		target, _ := url.Parse(tt.target)
		jar.SetCookies(source, []*http.Cookie{{Name: "test", Value: "example", Domain: tt.domain, Path: "/", Secure: true}})
		if len(jar.Cookies(target)) != 0 {
			t.Fatalf("cross-site cookie accepted for public suffix %q", tt.domain)
		}
	}
	jar := mediaClient().Jar
	source, _ := url.Parse("https://www.pornhub.com/")
	target, _ := url.Parse("https://pornhub.com/")
	jar.SetCookies(source, []*http.Cookie{{Name: "test", Value: "example", Domain: "pornhub.com", Path: "/", Secure: true}})
	if len(jar.Cookies(target)) != 1 {
		t.Fatal("legitimate same-site cookie was rejected")
	}
}
