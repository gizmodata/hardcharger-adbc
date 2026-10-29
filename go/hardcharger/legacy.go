// Licensed under the Apache License, Version 2.0.

package hardcharger

import "strings"

// Names from before the protocol was renamed to Chargewire keep working: the
// old URI scheme and option-key prefix are read as their current spellings,
// silently, before this package looks at them (the Chargewire driver reads
// them too, and falls back to servers from before the rename by itself).
// This file is the only place that knows the old names.
const (
	legacyScheme       = "warp"
	legacyOptionPrefix = "adbc.warp."
	currentScheme      = "chargewire"
	currentPrefix      = "adbc.chargewire."
)

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

// currentOptionKey returns key with an old option-key prefix replaced by the
// current one; any other key is returned unchanged.
func currentOptionKey(key string) string {
	if rest, ok := strings.CutPrefix(key, legacyOptionPrefix); ok {
		return currentPrefix + rest
	}
	return key
}

// currentOptions returns options with every old option key replaced by the
// current one. When a map holds both spellings of a key, the current one wins.
func currentOptions(options map[string]string) map[string]string {
	out := make(map[string]string, len(options))
	for key, value := range options {
		if current := currentOptionKey(key); current != key {
			if _, both := options[current]; both {
				continue
			}
			out[current] = value
			continue
		}
		out[key] = value
	}
	return out
}
