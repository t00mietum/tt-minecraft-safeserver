// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright © 2026 t00mietum (CryptogID: ปʬϝღถɔ4რఠΔթะ9ƾǝu)

package agent

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Fetcher is the one tool the API kinds get: a GET of an https page, given
// back as text. See design.md's Update agent.
type Fetcher struct {
	// Client is nil for one that only reaches public addresses.
	Client *http.Client
	// MaxPage is 64 KiB when zero, since every page stays in the agent's
	// context for the rest of the run. MaxFetches is 40.
	MaxPage    int64
	MaxFetches int
	done       int
}

const fetchSpec = `{"type": "object", "properties": {"url": {"type": "string", "description": "https address to read"}}, "required": ["url"]}`

const fetchAbout = "Reads a web page or API answer over https with GET, and gives back its text."

// Fetch gives the page's text, or what went wrong, for the agent to read.
func (f *Fetcher) Fetch(ctx context.Context, address string) string {
	limit := f.MaxFetches
	if limit == 0 {
		limit = 40
	}
	if f.done >= limit {
		return fmt.Sprintf("refused: this run's %d fetches are used up, so answer with what you have", limit)
	}
	f.done++
	text, err := f.get(ctx, address)
	if err != nil {
		return "fetch failed: " + err.Error()
	}
	return text
}

func (f *Fetcher) get(ctx context.Context, address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", errors.New("only https addresses, with no user name, can be fetched")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "tt-minecraftd")
	client := f.Client
	if client == nil {
		client = publicClient()
	}
	resp, err := client.Do(req) // #nosec G107 G704 -- https only, to public addresses only, see publicOnly
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }() // read-only, nothing to lose
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if kind != "" && !strings.HasPrefix(kind, "text/") && !strings.HasSuffix(kind, "json") &&
		!strings.HasSuffix(kind, "xml") {
		return "", fmt.Errorf("it's %s, not text", kind)
	}
	page := f.MaxPage
	if page == 0 {
		page = 64 << 10
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, page+1))
	if err != nil {
		return "", err
	}
	cut := int64(len(body)) > page
	if cut {
		body = body[:page]
	}
	text := strings.ToValidUTF8(string(body), "")
	if kind == "text/html" || kind == "application/xhtml+xml" {
		text = pageText(text)
	}
	if cut {
		text += fmt.Sprintf("\n[cut at %d bytes]", page)
	}
	return text, nil
}

var (
	unseen     = regexp.MustCompile(`(?is)<(script|style|noscript|svg)\b.*?</(script|style|noscript|svg)\s*>|<!--.*?-->`)
	link       = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*["']([^"']+)["'][^>]*>`)
	tag        = regexp.MustCompile(`(?s)<[^>]*>`)
	blankLines = regexp.MustCompile(`\n\s*\n+`)
	spaces     = regexp.MustCompile(`[ \t\r\f\v]+`)
)

// pageText is a rough reading of an HTML page, enough to find versions,
// hashes and links in. Links keep their address, since that's often the point.
func pageText(s string) string {
	s = unseen.ReplaceAllString(s, " ")
	s = link.ReplaceAllString(s, " [$1] ")
	s = tag.ReplaceAllString(s, "\n")
	s = html.UnescapeString(s)
	s = spaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(blankLines.ReplaceAllString(s, "\n"))
}

func publicClient() *http.Client { return PublicClient(30 * time.Second) }

// PublicClient only reaches public addresses, over https, with up to 5
// redirects. update fetches the agent's jars and hash pages with it.
func PublicClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, Control: publicOnly}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("redirected away from https")
			}
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

var sharedSpace = netip.MustParsePrefix("100.64.0.0/10")

// publicOnly runs on the address a name resolved to, just before the
// connect, so a name can't point fetch at this host or the LAN.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || sharedSpace.Contains(addr) {
		return fmt.Errorf("%s isn't a public address", addr)
	}
	return nil
}
