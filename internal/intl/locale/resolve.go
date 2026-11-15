// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"slices"
	"strings"
)

// ExtensionKey is a relevant extension key of a service constructor for ResolveLocale.
type ExtensionKey struct {
	// Key is the key of the Unicode locale extension, such as "nu".
	Key string
	// Option is the value of the option for the key, when HasOption is true.
	Option    string
	HasOption bool
	// OptionNull makes the option null, which is an empty string in Values.
	OptionNull bool
	// Values returns the values that the available locale l supports for the key, the
	// default first.
	Values func(l string) []string
}

// Resolution is the result of ResolveLocale.
type Resolution struct {
	// Locale is the [[Locale]] of the result: the available locale that matched, with the
	// keywords of the request that apply.
	Locale string
	// DataLocale is the available locale whose data applies.
	DataLocale string
	// Values are the resolved values of the keys, in the order of the keys.
	Values []string
}

// CanonicalizeUValue implements CanonicalizeUValue of ECMA-402.
func CanonicalizeUValue(key, value string) string {
	return canonicalValue(key, strings.ToLower(value))
}

// ResolveLocale implements ResolveLocale of ECMA-402 for the canonical language tags
// requested and the available locales. bestFit selects the "best fit" matcher, which is the
// lookup matcher.
func ResolveLocale(requested []string, bestFit bool, keys []ExtensionKey) Resolution {
	m := LookupMatcher(requested)
	found := m.Locale
	r := Resolution{
		DataLocale: found,
	}
	var supported []Keyword
	for _, k := range keys {
		values := k.Values(found)
		value := values[0]
		var keyword *Keyword
		if v, ok := m.Requested.Keyword(k.Key); ok {
			switch {
			case v != "" && slices.Contains(values, v):
				value = v
				keyword = &Keyword{
					Key:   k.Key,
					Value: v,
				}
			case v == "" && slices.Contains(values, "true"):
				value = "true"
				keyword = &Keyword{
					Key: k.Key,
				}
			}
		}
		if k.HasOption {
			var o string
			if !k.OptionNull {
				o = CanonicalizeUValue(k.Key, k.Option)
				if o == "" {
					o = "true"
				}
			}
			if o != value && slices.Contains(values, o) {
				value = o
				keyword = nil
			}
		}
		if keyword != nil {
			supported = append(supported, *keyword)
		}
		r.Values = append(r.Values, value)
	}
	if len(supported) > 0 {
		t, _ := Parse(found)
		t.HasUnicode = true
		t.Keywords = supported
		found = t.Canonicalize().String()
	}
	r.Locale = found
	return r
}
