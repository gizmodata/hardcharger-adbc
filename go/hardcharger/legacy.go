// Licensed under the Apache License, Version 2.0.

package hardcharger

import (
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/gizmodata/chargewire-adbc/go/v2/chargewire"
)

// Names from before the protocol was renamed to Chargewire keep working: the
// old URI scheme is read as the current one, silently (the Chargewire driver
// falls back to servers from before the rename by itself). Option keys from
// before the ADBC driver style guide (adbc.hardcharger.*, adbc.chargewire.*
// and adbc.warp.*) are read as their current names, with a warning on stderr
// the first time a process uses each. This file is the only place that knows
// the old names.
const (
	legacyScheme  = "warp"
	currentScheme = "chargewire"

	oldOptionPrefix = "adbc.hardcharger."
)

// host makes the Chargewire driver take every one of its options as
// hardcharger.X too, and warn about its old names in Hardcharger's terms,
// except where Hardcharger has an option of its own under that name
// (hardcharger.auth.type, hardcharger.oauth.*): the Chargewire driver's is
// then chargewire.X only.
var host = chargewire.Host{Name: "hardcharger", OptionPrefix: optionPrefix, Own: isOwnOption}

// renamedOwnOptions maps the part after adbc.hardcharger. of Hardcharger's
// own options to their current names. adbc.hardcharger.setting.NAME and
// global_setting.NAME keep their part after the prefix.
var renamedOwnOptions = map[string]string{
	"auth_type":             OptionAuthType,
	"oauth.timeout_seconds": OptionOAuthTimeoutSeconds,
	"oauth.browser":         OptionOAuthBrowser,
	"oauth.share_sign_in":   OptionOAuthShareSignIn,
	"oauth.sign_in_key":     OptionOAuthSignInKey,
	"session_tag":           OptionSessionTag,
	"query_tag":             OptionQueryTag,
	"ingest.parts":          OptionIngestParts,
	"resume_timeout":        OptionResumeTimeout,
}

// renamedOwnOption returns the current name of one of Hardcharger's own
// options from before the style guide, and true; any other key, including an
// adbc.hardcharger. key that names no option, is returned as given.
func renamedOwnOption(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, oldOptionPrefix)
	if !ok {
		return key, false
	}
	if current, ok := renamedOwnOptions[rest]; ok {
		return current, true
	}
	for _, prefix := range [...]string{"setting.", "global_setting."} {
		if name, ok := strings.CutPrefix(rest, prefix); ok && name != "" {
			return optionPrefix + prefix + name, true
		}
	}
	return key, false
}

// currentOptionKey returns the name this package reads for key, with which it
// passes the Chargewire driver's options on: Hardcharger's own options under
// their current names (OptionAuthType, ...), and every Chargewire option,
// whether spelled hardcharger.X, chargewire.X or an old way, as chargewire.X.
// An old name warns once per process. Any other key is returned as given, so
// the error for an unknown option names what the caller wrote.
func currentOptionKey(key string) string {
	current, _ := currentOptionKeyRanked(key)
	return current
}

// currentOptionKeyRanked is currentOptionKey, also saying how key was
// spelled: 0 Hardcharger's current name, 1 the Chargewire driver's (or not an
// option), 2 an old name.
func currentOptionKeyRanked(key string) (string, int) {
	if current, renamed := renamedOwnOption(key); renamed {
		chargewire.WarnDeprecatedOption(host.Name, key, current)
		if isOwnOption(current) {
			return current, 2
		}
		return host.CurrentOptionKey(current), 2 // ingest.parts, resume timeout
	}
	if isOwnOption(key) {
		return key, 0
	}
	if _, renamed := chargewire.RenamedOption(key); renamed {
		return host.CurrentOptionKey(key), 2 // warns in Hardcharger's terms
	}
	current := host.CurrentOptionKey(key)
	if current != key {
		return current, 0 // hardcharger.X for the Chargewire driver's chargewire.X
	}
	return key, 1
}

// currentOptions returns options under the names currentOptionKey gives. When
// several keys name one option, Hardcharger's spelling wins over the
// Chargewire driver's, and either over an old name, whatever the map's order.
func currentOptions(options map[string]string) map[string]string {
	out, _ := currentOptionsSpelled(options)
	return out
}

// currentOptionsSpelled is currentOptions, also giving the key the caller
// wrote for each option kept, so errors can name it.
func currentOptionsSpelled(options map[string]string) (map[string]string, map[string]string) {
	out := make(map[string]string, len(options))
	spelled := make(map[string]string, len(options))
	rank := make(map[string]int, len(options))
	for _, key := range slices.Sorted(maps.Keys(options)) {
		current, r := currentOptionKeyRanked(key)
		if previous, seen := rank[current]; seen && previous <= r {
			continue
		}
		out[current], rank[current], spelled[current] = options[key], r, key
	}
	return out, spelled
}

// asSpelled returns err with the name this package passed the option on
// under (chargewire.X) replaced by the key the caller wrote, so the error
// names what they set.
func asSpelled(err error, internal, given string) error {
	if err == nil || internal == given {
		return err
	}
	var adbcErr adbc.Error
	if errors.As(err, &adbcErr) {
		adbcErr.Msg = strings.ReplaceAll(adbcErr.Msg, internal, given)
		return adbcErr
	}
	return err
}

// ToWarpURI is ToChargewireURI under its name from before the rename, and
// with that name's result: the old URI scheme, which the driver still reads.
//
// Deprecated: use ToChargewireURI.
func ToWarpURI(raw string) (string, error) {
	translated, err := ToChargewireURI(raw)
	if err != nil {
		return "", err
	}
	if rest, ok := strings.CutPrefix(translated, currentScheme+"://"); ok {
		return legacyScheme + "://" + rest, nil
	}
	return translated, nil
}

// currentURI returns raw with an old URI scheme (in any case) replaced by the
// current one; any other URI is returned unchanged.
func currentURI(raw, scheme string) string {
	if strings.EqualFold(scheme, legacyScheme) {
		return currentScheme + raw[len(scheme):]
	}
	return raw
}
