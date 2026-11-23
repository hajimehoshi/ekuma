// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package cldr

import (
	"iter"
	"strconv"
)

// RelatedLanguages yields related written languages and their CLDR distances from desired, closest first.
func RelatedLanguages(desired string) iter.Seq2[string, int] {
	return func(yield func(string, int) bool) {
		record, _ := languageMatches.Lookup(desired)
		for language, value := range Fields(record) {
			distance, _ := strconv.Atoi(value)
			if !yield(language, distance) {
				return
			}
		}
	}
}

// ScriptDistance returns the CLDR matching distance between scripts of the same language.
func ScriptDistance(language, desired, supported string) int {
	if desired == supported {
		return 0
	}
	if distance, ok := scriptDistances[[3]string{language, desired, supported}]; ok {
		return distance
	}
	return DefaultScriptDistance
}

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

// RegionDistance returns the CLDR matching distance between regions for a language and script.
// An empty script selects the language-wide rules. An empty language selects the default rule.
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
