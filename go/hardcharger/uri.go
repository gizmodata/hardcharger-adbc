// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

const (
	// Scheme is Hardcharger's URI scheme: hardcharger://host[:port] means
	// exactly chargewire://host:port (HTTPS unless ?tls=false).
	Scheme = "hardcharger"
	// DefaultPort is Hardcharger's Chargewire port, used when a hardcharger://
	// URI names none (chargewire:// would default to 443).
	DefaultPort = 31337
)

// ToChargewireURI translates a hardcharger:// URI into the chargewire:// URI the Chargewire
// driver reads, adding the default port 31337 when the URI has none.
// chargewire://, https:// and http:// URIs pass through unchanged; the Chargewire
// driver validates everything else (parameters, credentials in the URI).
func ToChargewireURI(raw string) (string, error) {
	if raw == "" {
		return "", invalidArgument("a uri is required, e.g. hardcharger://host:31337")
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Error repeats the URI, password included: report only the cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return "", invalidArgument("invalid uri: %v", err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == Scheme:
	case strings.HasPrefix(scheme, Scheme+"+"):
		return "", invalidArgument("%s:// is not a Hardcharger URI scheme: use hardcharger://host:port for HTTPS, "+
			"or hardcharger://host:port?tls=false for plain HTTP", u.Scheme)
	default:
		return raw, nil // chargewire://, https://, http://, or something the Chargewire driver refuses
	}
	if u.Hostname() == "" {
		return "", invalidArgument("invalid uri %q: no host", u.Redacted())
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(DefaultPort))
	}
	u.Scheme = "chargewire"
	return u.String(), nil
}

// usesTLS reports whether a (translated) Chargewire URI connects over TLS.
func usesTLS(chargewireURI string) bool {
	u, err := url.Parse(chargewireURI)
	if err != nil {
		return true
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return false
	case "chargewire":
		switch strings.ToLower(strings.TrimSpace(u.Query().Get("tls"))) {
		case "false", "0", "f", "no", "off":
			return false
		}
	}
	return true
}

func invalidArgument(format string, args ...any) error {
	return adbc.Error{Code: adbc.StatusInvalidArgument, Msg: "[hardcharger] " + fmt.Sprintf(format, args...)}
}
