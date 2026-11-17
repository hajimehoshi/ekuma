// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package intl adds the ECMAScript Internationalization API (ECMA-402) to the VMs of package
// ekuma. Import it for its side effect:
//
//	import _ "github.com/hajimehoshi/ekuma/intl"
//
// The global object of every VM then has Intl, with the data of every locale of the Unicode
// Common Locale Data Repository, and Temporal has the calendars of ECMA-402 besides ISO 8601.
// Without the import, a VM has no Intl, and Temporal has only the ISO 8601 calendar.
//
// That data is under the Unicode License v3. A program that imports the package must include
// the copyright and permission notice of that license, in internal/unicode/cldr/LICENSE and
// internal/unicode/collation/LICENSE, with its copies or in their documentation.
//
// VM.SetLocale overrides the default locale. Otherwise, the first nonempty LC_ALL,
// LC_MESSAGES or LANG environment variable selects it. With none set, the default is
// the user locale on macOS, iOS and Windows, the system locale on Android, or the
// system locale configuration on Linux. BSD systems and Android builds without Cgo
// use the locale environment variables.
// Browser builds use navigator.language. An unavailable host locale, C or POSIX uses en-US.
package intl

import (
	"github.com/hajimehoshi/ekuma/internal/builtin"
	intlbuiltin "github.com/hajimehoshi/ekuma/internal/builtin/intl"
	"github.com/hajimehoshi/ekuma/internal/intl/calendar"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

func init() {
	temporal.SetCalendars(calendar.Temporal)
	builtin.RegisterExtension(intlbuiltin.Install)
}
