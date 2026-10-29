// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Hardcharger's ADBC options. Every other option is the Chargewire driver's
// (adbc.chargewire.*, uri, username, password, ...).
const (
	optionPrefix = "adbc.hardcharger."

	// OptionAuthType is "password" (the default; also for a pre-issued
	// access token), "external": sign in through a browser with the server's
	// identity provider (Chargewire browser sign-in on the server's own port;
	// hardcharger-server --oauth-client-id), when the first connection opens, or "client_certificate" ("mtls"): sign in with the
	// TLS client certificate alone (adbc.chargewire.tls.client_cert and
	// adbc.chargewire.tls.client_key), no password; the server must run with
	// --mtls-ca-cert and --mtls-certificate-sign-in. It is the Chargewire
	// driver's adbc.chargewire.auth.type, which does the sign-in, under
	// Hardcharger's name.
	OptionAuthType            = "adbc.hardcharger.auth_type"
	AuthTypePassword          = "password"
	AuthTypeExternal          = "external"
	AuthTypeClientCertificate = "client_certificate"
	// authTypeMTLS is accepted for client_certificate, as the hardcharger
	// command-line client's --auth-type mtls.
	authTypeMTLS       = "mtls"
	chargewireAuthType = "adbc.chargewire.auth.type"

	// OptionOAuthTimeoutSeconds is how long to wait for the user to sign in
	// (default 300).
	OptionOAuthTimeoutSeconds = "adbc.hardcharger.oauth.timeout_seconds"
	// OptionOAuthBrowser is the command that opens the sign-in page, given
	// its URL as the last argument. Default: $HARDCHARGER_BROWSER, else the
	// system's (open, xdg-open, rundll32). "none" only prints the URL to
	// stderr.
	OptionOAuthBrowser = "adbc.hardcharger.oauth.browser"
	// OptionOAuthShareSignIn ("true"/"false", default "false"): share one
	// browser sign-in among the databases of this process that sign in to the
	// same server, with the same TLS trust and OptionOAuthSignInKey. The first
	// to open a connection signs in (the others wait for it); the rest reuse
	// its access token until shortly before it expires, then the next one
	// signs in again. A shared token the server
	// refuses is dropped and that database signs in again. For applications
	// that open a database per connection (dbt, pools, the Python DB-API's
	// connect()), which would otherwise open a browser window for each.
	// ForgetSharedSignIns drops them all. auth_type external only.
	OptionOAuthShareSignIn = "adbc.hardcharger.oauth.share_sign_in"
	// OptionOAuthSignInKey names a shared sign-in (default ""): databases
	// share one only when their keys match, so a process that signs in as two
	// people (or two roles) to one server gives each its own key.
	OptionOAuthSignInKey = "adbc.hardcharger.oauth.sign_in_key"

	// OptionSessionTag is a JSON object tagging the session
	// (hardcharger.session_tag) in Hardcharger's instrumentation.
	OptionSessionTag = "adbc.hardcharger.session_tag"
	// OptionQueryTag is a JSON object recorded with each of the session's
	// following statements (hardcharger.query_tag).
	OptionQueryTag = "adbc.hardcharger.query_tag"
	// OptionSettingPrefix + NAME sets the session setting hardcharger.NAME
	// (SET SESSION); an empty value resets it. On a connection, GetOption
	// reads the value that applies to the session.
	OptionSettingPrefix = "adbc.hardcharger.setting."
	// OptionIngestParts is the Chargewire driver's adbc.chargewire.ingest.parts under
	// Hardcharger's name: the most connections one bulk ingest is sent over at
	// once (default 8: an upload of two batches or more is a partitioned
	// ingest; 1: one ordered stream). Database and connection; on a statement,
	// set adbc.chargewire.ingest.parts.
	OptionIngestParts     = "adbc.hardcharger.ingest.parts"
	chargewireIngestParts = "adbc.chargewire.ingest.parts"

	// OptionGlobalSettingPrefix + NAME sets the server-wide setting
	// hardcharger.NAME (SET GLOBAL; admins only); an empty value resets it.
	// Connections only.
	OptionGlobalSettingPrefix = "adbc.hardcharger.global_setting."
)

const defaultOAuthTimeout = 300 * time.Second

var settingNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// setting is one hardcharger.NAME = VALUE, in the order it was given.
type setting struct {
	name, value string
}

// config holds a database's Hardcharger options.
type config struct {
	authType     string
	oauthTimeout time.Duration
	browser      string
	shareSignIn  bool
	signInKey    string
	sessionTag   *string
	queryTag     *string
	settings     []setting
}

func newConfig() *config {
	return &config{authType: AuthTypePassword, oauthTimeout: defaultOAuthTimeout}
}

func isHardchargerOption(key string) bool { return strings.HasPrefix(key, optionPrefix) }

// settingName validates NAME (also accepting hardcharger.NAME); it becomes
// part of a SET statement, so only [a-z0-9_] are allowed.
func settingName(name string) (string, error) {
	bare := strings.TrimPrefix(name, "hardcharger.")
	if !settingNamePattern.MatchString(bare) {
		return "", invalidArgument("invalid Hardcharger setting name %q: expected a name such as "+
			"query_timeout (lower-case letters, digits and underscores)", name)
	}
	return bare, nil
}

func (c *config) set(key, value string) error {
	switch {
	case key == OptionAuthType:
		switch value {
		case AuthTypePassword, AuthTypeExternal, AuthTypeClientCertificate:
			c.authType = value
		case authTypeMTLS:
			c.authType = AuthTypeClientCertificate
		default:
			return invalidArgument("invalid value %q for %s: expected password, external or client_certificate (mtls)",
				value, key)
		}
	case key == OptionOAuthTimeoutSeconds:
		seconds, err := strconv.ParseFloat(value, 64)
		if err != nil || seconds <= 0 {
			return invalidArgument("invalid value %q for %s: expected a number of seconds", value, key)
		}
		c.oauthTimeout = time.Duration(seconds * float64(time.Second))
	case key == OptionOAuthBrowser:
		c.browser = value
	case key == OptionOAuthShareSignIn:
		switch strings.ToLower(value) {
		case "true", "1":
			c.shareSignIn = true
		case "false", "0", "":
			c.shareSignIn = false
		default:
			return invalidArgument("invalid value %q for %s: expected true or false", value, key)
		}
	case key == OptionOAuthSignInKey:
		c.signInKey = value
	case key == OptionSessionTag:
		c.sessionTag = &value
	case key == OptionQueryTag:
		c.queryTag = &value
	case strings.HasPrefix(key, OptionSettingPrefix):
		name, err := settingName(strings.TrimPrefix(key, OptionSettingPrefix))
		if err != nil {
			return err
		}
		c.settings = append(c.settings, setting{name: name, value: value})
	case strings.HasPrefix(key, OptionGlobalSettingPrefix):
		return invalidArgument("%s is a connection option: set it on an open connection", key)
	default:
		return adbc.Error{Code: adbc.StatusNotImplemented, Msg: "[hardcharger] unknown option " + key}
	}
	return nil
}

func (c *config) get(key string) (string, error) {
	switch {
	case key == OptionAuthType:
		return c.authType, nil
	case key == OptionOAuthTimeoutSeconds:
		return strconv.FormatFloat(c.oauthTimeout.Seconds(), 'g', -1, 64), nil
	case key == OptionOAuthBrowser:
		return c.browser, nil
	case key == OptionOAuthShareSignIn:
		return strconv.FormatBool(c.shareSignIn), nil
	case key == OptionOAuthSignInKey:
		return c.signInKey, nil
	case key == OptionSessionTag && c.sessionTag != nil:
		return *c.sessionTag, nil
	case key == OptionQueryTag && c.queryTag != nil:
		return *c.queryTag, nil
	case strings.HasPrefix(key, OptionSettingPrefix):
		name := strings.TrimPrefix(strings.TrimPrefix(key, OptionSettingPrefix), "hardcharger.")
		for i := len(c.settings) - 1; i >= 0; i-- {
			if c.settings[i].name == name {
				return c.settings[i].value, nil
			}
		}
	}
	return "", adbc.Error{Code: adbc.StatusNotFound, Msg: "[hardcharger] option " + key + " is not set"}
}

// chargewireAuthTypeFor is the Chargewire driver's adbc.chargewire.auth.type for an
// auth_type: a browser sign-in here hands the Chargewire driver the server's
// access token, which it uses as a token issued elsewhere.
func chargewireAuthTypeFor(authType string) string {
	if authType == AuthTypeClientCertificate {
		return AuthTypeClientCertificate
	}
	return AuthTypePassword
}

// sqlString quotes value as a SQL string literal.
func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// setStatement is SET (or, for an empty value, RESET) of hardcharger.NAME.
func setStatement(name, value string, global bool) string {
	scope := "SESSION"
	if global {
		scope = "GLOBAL"
	}
	if value == "" {
		return "RESET " + scope + " hardcharger." + name
	}
	return "SET " + scope + " hardcharger." + name + " = " + sqlString(value)
}
