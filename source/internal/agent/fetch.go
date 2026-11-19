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

	"github.com/t00mietum/tt-minecraft-safeserver/source/internal/download"
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
	public     *http.Client // made once, so fetches share connections
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
		if f.public == nil {
			f.public = publicClient()
		}
		client = f.public
	}
	resp, err := client.Do(req) // #nosec G107 G704 -- https only, to public addresses only, see publicOnly
	if err != nil {
		return "", untold(err)
	}
	defer func() { _ = resp.Body.Close() }() // read-only, nothing to lose
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", download.Status(resp.StatusCode))
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
// redirects. update fetches the agent's jars and hash pages with it, through
// HoldTo.
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

// HoldTo is a copy of c that only follows a redirect to a host in hosts, by
// the same rules as the agent's first address. Otherwise a redirect could
// take a jar anywhere, while the plan names the host it was found on.
func HoldTo(c *http.Client, hosts []string) *http.Client {
	own := *c
	before := c.CheckRedirect
	own.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if before != nil {
			if err := before(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 { // Go's own limit, which setting this drops
			return errors.New("too many redirects")
		}
		if err := checkAddress(req.URL.String(), hosts); err != nil {
			return fmt.Errorf("redirected, but %w", err)
		}
		return nil
	}
	return &own
}

var errNotPublic = errors.New("the name doesn't lead to a public address")

// Not public, though IsGlobalUnicast and IsPrivate let them by.
var notPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // shared, carrier NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"), // old IPv4-compatible form; Unmap took the mapped one
	netip.MustParsePrefix("::ffff:0:0:0/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"), // Teredo, whose relay can reach the LAN
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fec0::/10"),
}

var (
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour = netip.MustParsePrefix("2002::/16")
)

func public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(addr) {
			return false
		}
	}
	// These carry an IPv4 address, which a gateway on the path connects to.
	b := addr.As16()
	switch {
	case nat64.Contains(addr):
		return public(netip.AddrFrom4([4]byte(b[12:16])))
	case sixToFour.Contains(addr):
		return public(netip.AddrFrom4([4]byte(b[2:6])))
	}
	return true
}

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
	if !public(addr) {
		return errNotPublic
	}
	return nil
}

// untold is why a fetch failed, without the addresses Go's errors name: the
// one a name resolved to, or this host's resolver. design.md's Update agent
// says the agent isn't told the host's addresses.
func untold(err error) error {
	var dns *net.DNSError
	var op *net.OpError
	var ue *url.Error
	switch {
	case errors.Is(err, errNotPublic):
		return fmt.Errorf("refused, since %w", errNotPublic)
	case errors.As(err, &dns):
		if dns.IsTimeout {
			return fmt.Errorf("looking up %s timed out", dns.Name)
		}
		return fmt.Errorf("can't look up %s", dns.Name)
	case errors.As(err, &op):
		return fmt.Errorf("can't connect: %w", op.Err)
	case errors.As(err, &ue):
		return ue.Err
	}
	return err
}
