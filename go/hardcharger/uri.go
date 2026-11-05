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
	// URI names none (chargewire:// would default to 443), except for a
	// GizmoData Cloud host (GizmoDataCloudPort).
	DefaultPort = 31337
	// GizmoDataCloudPort is the port a hardcharger:// URI without one gets
	// for a GizmoData Cloud host (gizmodata.com or a name under it): its
	// clusters answer on 443.
	GizmoDataCloudPort = 443
	// gizmoDataCloudDomain is GizmoData Cloud's domain.
	gizmoDataCloudDomain = "gizmodata.com"
)

// IsGizmoDataCloudHost reports whether host is gizmodata.com or a name under
// it, in any case, with or without the trailing dot of a fully qualified name.
// It matches at a label boundary: evilgizmodata.com and
// gizmodata.com.example.org are not GizmoData Cloud hosts. The Hardcharger CLI
// (server/client/client_config.cc) uses the same rule.
func IsGizmoDataCloudHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == gizmoDataCloudDomain {
		return true
	}
	return len(host) > len(gizmoDataCloudDomain)+1 && strings.HasSuffix(host, "."+gizmoDataCloudDomain)
}

// ToChargewireURI translates a hardcharger:// URI into the chargewire:// URI the Chargewire
// driver reads, adding the default port when the URI has none: 31337, or 443 for a
// GizmoData Cloud host (IsGizmoDataCloudHost). An explicit port always wins.
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
		return currentURI(raw, u.Scheme), nil // chargewire://, https://, http://, or something the Chargewire driver refuses
	}
	if u.Hostname() == "" {
		return "", invalidArgument("invalid uri %q: no host", u.Redacted())
	}
	if u.Port() == "" {
		port := DefaultPort
		if IsGizmoDataCloudHost(u.Hostname()) {
			port = GizmoDataCloudPort
		}
		u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	}
	u.Scheme = currentScheme
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
