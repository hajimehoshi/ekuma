// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"bytes"
	"sync"

	"github.com/ebitengine/purego"
)

type coreFoundationLocaleAPI struct {
	copyCurrent   func() uintptr
	getIdentifier func(uintptr) uintptr
	getLength     func(uintptr) int
	getCString    func(uintptr, []byte, int, uint32) bool
	release       func(uintptr)
}

var coreFoundationLocale = sync.OnceValue(func() *coreFoundationLocaleAPI {
	library, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	var api coreFoundationLocaleAPI
	for _, binding := range []struct {
		name     string
		function any
	}{
		{
			name:     "CFLocaleCopyCurrent",
			function: &api.copyCurrent,
		},
		{
			name:     "CFLocaleGetIdentifier",
			function: &api.getIdentifier,
		},
		{
			name:     "CFStringGetLength",
			function: &api.getLength,
		},
		{
			name:     "CFStringGetCString",
			function: &api.getCString,
		},
		{
			name:     "CFRelease",
			function: &api.release,
		},
	} {
		symbol, err := purego.Dlsym(library, binding.name)
		if err != nil {
			purego.Dlclose(library)
			return nil
		}
		purego.RegisterFunc(binding.function, symbol)
	}
	// The library stays loaded for the lifetime of the function bindings.
	return &api
})

func systemLocale() string {
	api := coreFoundationLocale()
	if api == nil {
		return ""
	}
	locale := api.copyCurrent()
	if locale == 0 {
		return ""
	}
	defer api.release(locale)
	identifier := api.getIdentifier(locale)
	if identifier == 0 {
		return ""
	}
	// Each UTF-16 code unit requires at most three UTF-8 bytes.
	buffer := make([]byte, 3*api.getLength(identifier)+1)
	const utf8Encoding = 0x08000100
	if !api.getCString(identifier, buffer, len(buffer), utf8Encoding) {
		return ""
	}
	return string(buffer[:bytes.IndexByte(buffer, 0)])
}
