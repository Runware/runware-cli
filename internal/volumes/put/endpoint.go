package put

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/runware/runware-cli/internal/config"
	"golang.org/x/net/idna"
)

var defaultEndpoint, _ = endpointIdentity(config.DefaultServerlessBaseURL)

// endpointIdentity scopes a journal to the configured API, including any base
// path. The digest keeps the server address out of the journal.
func endpointIdentity(raw string) (string, error) {
	u, err := url.Parse(raw)
	invalid := errors.New("invalid API base URL for volume uploads")
	if err != nil || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", invalid
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	switch scheme {
	case "http":
		if port == "" {
			port = "80"
		}
	case "https":
		if port == "" {
			port = "443"
		}
	default:
		return "", invalid
	}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		host = addr.String()
	} else {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil {
			return "", invalid
		}
		host = strings.ToLower(host)
	}
	base := scheme + "://" + net.JoinHostPort(host, port) + strings.TrimSuffix(u.EscapedPath(), "/")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(base))), nil
}
