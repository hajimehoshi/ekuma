// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package intl adds the ECMAScript Internationalization API (ECMA-402) to the VMs of package
// ekuma. Import it for its side effect:
//
//	import _ "github.com/hajimehoshi/ekuma/intl"
//
// The global object of every VM then has Intl, with the data of every locale of the Unicode
// Common Locale Data Repository. Without the import, a VM has no Intl.
//
// That data is under the Unicode License v3. A program that imports the package must include
// the copyright and permission notice of that license, in internal/unicode/cldr/LICENSE, with
// its copies or in their documentation.
//
// The default locale is that of the environment: the LC_ALL, LC_MESSAGES or LANG
// environment variable natively, and navigator.language on GOOS=js. It is en-US when the
// environment names no locale, names the C or POSIX locale, or names one without data.
package intl

import (
	"github.com/hajimehoshi/ekuma/internal/builtin"
	intlbuiltin "github.com/hajimehoshi/ekuma/internal/builtin/intl"
)

func init() {
	builtin.RegisterExtension(intlbuiltin.Install)
}
