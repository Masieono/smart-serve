// Package urlguard holds the one rule this app applies to a URL it was asked to fetch.
//
// Two places take a URL from configuration and call it: a notification channel and the knowledge
// base refresh. Both need the same answer to the same question, and both would drift if each carried
// its own copy of it, so it lives here.
//
// The rule is deliberately narrow. This app is self-hosted on a home or office network, and the
// normal destination for both features *is* that network: ntfy on the same host, Gotify on the LAN,
// a knowledge file on a NAS. So private and loopback addresses are allowed. What is refused is the
// traffic that is never a notification or a knowledge document: the link-local range, where cloud
// metadata services live, and redirects, which turn one configured destination into whatever the
// remote server chooses to send this app to next.
//
// It judges the URL, not what DNS answers: a hostname that resolves to a link-local address is not
// caught here. That is a narrower hole than an open redirect, since it needs control of a name the
// operator configured, but it is why this is a guard and not a sandbox.
package urlguard

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Check refuses the destinations that are never legitimate here.
func Check(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("no destination")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("refusing a %s URL", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("no host in that URL")
	}
	// The name, not just the address: a resolver can point this at the metadata service.
	if strings.EqualFold(host, "metadata.google.internal") ||
		strings.EqualFold(host, "metadata.goog") ||
		strings.EqualFold(host, "instance-data") {
		return fmt.Errorf("refusing to fetch %s", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		// Link-local is where the cloud metadata endpoints live, and 0.0.0.0 is not a destination.
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("refusing to fetch %s", host)
		}
	}
	return nil
}

// Client returns an HTTP client that refuses redirects.
//
// A redirect is reported as the response it is, so the caller sees a 3xx and can say so, rather than
// silently being moved to a second destination nobody configured.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
