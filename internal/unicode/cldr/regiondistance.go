// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package cldr

type regionSet struct {
	regions Strings
	exclude bool
}

func (s regionSet) contains(region string) bool {
	return s.regions.Contains(region) != s.exclude
}

type regionDistanceRule struct {
	desired, supported regionSet
	distance           int
}

// RegionDistance returns the CLDR matching distance between regions of the same maximized language and script.
func RegionDistance(language, script, desired, supported string) int {
	if desired == supported {
		return 0
	}
	for _, key := range [...]string{language + "-" + script, language} {
		for _, rule := range regionDistanceRules[key] {
			if rule.desired.contains(desired) && rule.supported.contains(supported) {
				return rule.distance
			}
		}
	}
	return regionDistanceRules["*"][0].distance
}
