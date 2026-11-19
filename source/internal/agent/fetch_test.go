// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/testid"
)

func pages(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/page", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><head><style>p{}</style><script>var key="secret";</script></head>
<body><h1>Chunky 1.4.40</h1><!-- old: 1.4.28 -->
<p>SHA-256: <code>`+sum256+`</code> &amp; more</p>
<a href="https://cdn.modrinth.com/c.jar">Download</a></body></html>`)
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"version": "1.4.40"}`)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, strings.Repeat("x", 300<<10))
	})
	mux.HandleFunc("/jar", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/java-archive")
		_, _ = fmt.Fprint(w, "PK")
	})
	ts := httptest.NewTLSServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestFetchReadsText(t *testing.T) {
	defer testid.Line(t, "df76600")
	ts := pages(t)
	// 9 fetches below, refused ones included, then one past the cap.
	f := &Fetcher{Client: ts.Client(), MaxFetches: 9}
	ctx := context.Background()

	page := f.Fetch(ctx, ts.URL+"/page")
	for _, want := range []string{"Chunky 1.4.40", "SHA-256:", sum256, "& more", "[https://cdn.modrinth.com/c.jar]", "Download"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	for _, unwanted := range []string{"secret", "old: 1.4.28", "<p>", "p{}"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("page has %q:\n%s", unwanted, page)
		}
	}
	if got := f.Fetch(ctx, ts.URL+"/json"); got != `{"version": "1.4.40"}` {
		t.Errorf("json: %q", got)
	}
	if got := f.Fetch(ctx, ts.URL+"/big"); len(got) > 65<<10 || !strings.HasSuffix(got, "[cut at 65536 bytes]") {
		t.Errorf("big: %d bytes, ending %q", len(got), got[len(got)-30:])
	}
	for path, says := range map[string]string{
		"/jar":     "application/java-archive, not text",
		"/missing": "HTTP 404",
	} {
		if got := f.Fetch(ctx, ts.URL+path); !strings.HasPrefix(got, "fetch failed: ") || !strings.Contains(got, says) {
			t.Errorf("%s: %q", path, got)
		}
	}
	plain := strings.Replace(ts.URL, "https://", "http://", 1)
	user := strings.Replace(ts.URL, "https://", "https://me@", 1)
	for _, address := range []string{plain + "/json", user + "/json", "file:///etc/passwd", "/json"} {
		if got := f.Fetch(ctx, address); !strings.Contains(got, "only https addresses") {
			t.Errorf("%s: %q", address, got)
		}
	}
	if got := f.Fetch(ctx, ts.URL+"/json"); !strings.Contains(got, "fetches are used up") {
		t.Errorf("past the cap: %q", got)
	}
}

func TestFetchOnlyReachesPublicAddresses(t *testing.T) {
	defer testid.Line(t, "df76700")
	refused := []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.123", "169.254.169.254",
		"fe80::1", "fd00::1", "100.100.100.100", "0.0.0.0", "::ffff:127.0.0.1", "::ffff:192.168.1.1", "224.0.0.1"}
	for _, addr := range refused {
		if err := publicOnly("tcp", "["+addr+"]:443", nil); !errors.Is(err, errNotPublic) {
			t.Errorf("%s: %v", addr, err)
		}
	}
	for _, addr := range []string{"1.1.1.1", "2606:4700::1111", "104.16.0.1"} {
		if err := publicOnly("tcp", "["+addr+"]:443", nil); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}
	// The default client, against a real server on loopback.
	ts := pages(t)
	if got := (&Fetcher{}).Fetch(context.Background(), ts.URL+"/json"); !strings.Contains(got, "doesn't lead to a public address") {
		t.Errorf("loopback server: %q", got)
	}
	check := publicClient().CheckRedirect
	to := func(s string) *http.Request {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Request{URL: u}
	}
	if err := check(to("http://example.com/"), make([]*http.Request, 1)); err == nil {
		t.Error("a redirect to http was followed")
	}
	if err := check(to("https://example.com/"), make([]*http.Request, 5)); err == nil {
		t.Error("a 6th redirect was followed")
	}
	if err := check(to("https://example.com/"), make([]*http.Request, 1)); err != nil {
		t.Errorf("https redirect: %v", err)
	}
}

// The forms that carry an IPv4 address are judged by it, the reserved ranges
// are refused, and the agent isn't told which address a name led to.
func TestFetchRefusesLANForms(t *testing.T) {
	defer testid.Line(t, "df7bs00")
	refused := []string{
		"64:ff9b::a00:1", "64:ff9b::7f00:1", "64:ff9b::c0a8:17b", "64:ff9b::a9fe:a9fe", "64:ff9b:1::a00:1",
		"2002:c0a8:101::1", "2002:7f00:1::1", "2002:a00:1::", "2001::1", "::7f00:1", "::a00:1", "::ffff:0:a00:1",
		"fec0::1", "100::1", "2001:db8::1",
		"0.1.2.3", "192.0.0.1", "192.0.2.1", "198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "240.0.0.1", "255.255.255.255",
	}
	for _, addr := range refused {
		if err := publicOnly("tcp", "["+addr+"]:443", nil); !errors.Is(err, errNotPublic) {
			t.Errorf("%s: %v", addr, err)
		}
	}
	for _, addr := range []string{"64:ff9b::101:101", "2002:101:101::1", "2a00:1450:4001::1", "8.8.8.8", "198.20.0.1"} {
		if err := publicOnly("tcp", "["+addr+"]:443", nil); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}

	ts := pages(t)
	got := (&Fetcher{}).Fetch(context.Background(), ts.URL+"/json")
	if got != "fetch failed: refused, since the name doesn't lead to a public address" {
		t.Errorf("loopback server: %q", got)
	}
	// Go's errors name the resolver, and both ends of a connection.
	lan := &net.TCPAddr{IP: net.ParseIP("192.168.1.5"), Port: 40000}
	far := &net.TCPAddr{IP: net.ParseIP("10.9.8.7"), Port: 443}
	for _, c := range []struct {
		err  error
		want string
	}{
		{&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "files.example", Server: "192.168.1.1:53", Err: "no such host", IsNotFound: true}}, "can't look up files.example"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "files.example", Server: "192.168.1.1:53", Err: "i/o timeout", IsTimeout: true}}, "looking up files.example timed out"},
		{&net.OpError{Op: "dial", Net: "tcp", Addr: far, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}, "can't connect: connect: connection refused"},
		{&net.OpError{Op: "read", Net: "tcp", Source: lan, Addr: far, Err: os.NewSyscallError("read", syscall.ECONNRESET)}, "can't connect: read: connection reset by peer"},
	} {
		err := untold(&url.Error{Op: "Get", URL: "https://files.example/x", Err: c.err})
		if err == nil || err.Error() != c.want || strings.Contains(err.Error(), "192.168") || strings.Contains(err.Error(), "10.9") {
			t.Errorf("%v: got %v, want %q", c.err, err, c.want)
		}
	}
}
