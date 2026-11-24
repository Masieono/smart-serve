package urlguard

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func parse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// The metadata endpoints are the reason this package exists: they are link-local, they answer
// unauthenticated, and a notification URL is a place an attacker can type.
func TestLinkLocalAndMetadataAreRefused(t *testing.T) {
	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://[fe80::1]/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://0.0.0.0:8080/",
	} {
		if err := Check(parse(t, raw)); err == nil {
			t.Errorf("%s was allowed; it is never a notification or knowledge destination", raw)
		}
	}
}

// Everything on the operator's own network is the normal case, so it stays allowed.
func TestPrivateAndLoopbackAreAllowed(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:2586/ntfy",
		"http://[::1]:8080/",
		"https://ntfy.lan/topic",
		"http://192.168.1.10:8080/message",
		"http://10.1.80.14/",
	} {
		if err := Check(parse(t, raw)); err != nil {
			t.Errorf("%s was refused: %v", raw, err)
		}
	}
}

func TestNonHTTPSchemesAreRefused(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.test/x", "gopher://x"} {
		if err := Check(parse(t, raw)); err == nil {
			t.Errorf("%s was allowed", raw)
		}
	}
}

// A redirect turns one configured destination into whatever the remote server picks next, which is
// how a hop crosses from a lane the operator chose to one they did not.
func TestRedirectsAreNotFollowed(t *testing.T) {
	var second int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second++
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer first.Close()

	resp, err := Client(0).Get(first.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want the 302 itself", resp.StatusCode)
	}
	if second != 0 {
		t.Errorf("the redirect was followed: %d request(s) reached the second host", second)
	}
}
