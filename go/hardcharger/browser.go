// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

// Browser sign-in (auth_type external) is the Chargewire protocol's (spec §4.3),
// on the server's own port: the Chargewire driver asks the server for a sign-in
// flow, the user signs in to the server's identity provider in a browser, and
// the server hands out its own access token once. The identity provider's
// settings stay on the server.

// openBrowser is the opener for OptionOAuthBrowser's value (tests replace it).
var openBrowser = func(command string) chargewire.BrowserOpener { return chargewire.SystemBrowser(command) }

// browserSignIn runs a browser sign-in against the server the Chargewire
// options name (uri and adbc.chargewire.tls.*) and returns the server's access
// token and its lifetime.
func browserSignIn(ctx context.Context, chargewireOptions map[string]string, browser string,
	timeout time.Duration) (string, time.Duration, error) {
	if browser == "" {
		browser = defaultBrowser()
	}
	result, err := chargewire.BrowserSignIn(ctx, chargewireOptions, openBrowser(browser), timeout)
	if err != nil {
		return "", 0, err
	}
	return result.AccessToken, result.ExpiresIn, nil
}

func oauthError(code adbc.Status, format string, args ...any) error {
	return adbc.Error{Code: code, Msg: "[hardcharger] browser sign-in: " + fmt.Sprintf(format, args...)}
}

// defaultBrowser is the browser command used when OptionOAuthBrowser is unset:
// $HARDCHARGER_BROWSER, else the system's.
func defaultBrowser() string { return os.Getenv("HARDCHARGER_BROWSER") }
