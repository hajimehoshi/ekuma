// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

func localeFromAndroidProperties(properties map[string]string) string {
	if locale := properties["persist.sys.locale"]; locale != "" {
		return locale
	}
	if language := properties["persist.sys.language"]; language != "" {
		if country := properties["persist.sys.country"]; country != "" {
			language += "-" + country
		}
		return language
	}
	if locale := properties["ro.product.locale"]; locale != "" {
		return locale
	}
	language := properties["ro.product.locale.language"]
	if language == "" {
		return ""
	}
	if region := properties["ro.product.locale.region"]; region != "" {
		language += "-" + region
	}
	return language
}
