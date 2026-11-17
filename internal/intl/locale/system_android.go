// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// TODO: Remove this constraint and the Android fallback in system_other.go when
// purego supports Android without Cgo. Its Android bindings currently require Cgo.

//go:build cgo

package locale

import (
	"sync"

	"github.com/ebitengine/purego"
)

var androidSystemPropertyGet = sync.OnceValue(func() func(string, *byte) int32 {
	library, err := purego.Dlopen("libc.so", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	symbol, err := purego.Dlsym(library, "__system_property_get")
	if err != nil {
		purego.Dlclose(library)
		return nil
	}
	var get func(string, *byte) int32
	purego.RegisterFunc(&get, symbol)
	// The library stays loaded for the lifetime of the function binding.
	return get
})

func systemLocale() string {
	get := androidSystemPropertyGet()
	if get == nil {
		return ""
	}
	properties := make(map[string]string)
	for _, key := range []string{
		"persist.sys.locale",
		"persist.sys.language",
		"persist.sys.country",
		"ro.product.locale",
		"ro.product.locale.language",
		"ro.product.locale.region",
	} {
		var value [92]byte // PROP_VALUE_MAX, including the terminating null.
		if n := get(key, &value[0]); n > 0 && n < int32(len(value)) {
			properties[key] = string(value[:n])
		}
	}
	return localeFromAndroidProperties(properties)
}
