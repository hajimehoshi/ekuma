// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package intl adds the ECMAScript Internationalization API (ECMA-402) to the VMs of package
// [github.com/hajimehoshi/ekuma]. Import it for its side effect:
//
//	import _ "github.com/hajimehoshi/ekuma/intl"
//
// The global object of every VM then has Intl, with data for every locale in the Unicode
// Common Locale Data Repository, and Temporal supports the ECMA-402 calendars in
// addition to ISO 8601.
// Without the import, a VM has no Intl, and Temporal has only the ISO 8601 calendar.
//
// Programs that import this package must distribute the notices for its CLDR data and
// ICU4C word dictionaries in internal/unicode/LICENSE.
//
// [github.com/hajimehoshi/ekuma.VM.SetLocale] overrides the default locale.
// Otherwise, the first nonempty LC_ALL,
// LC_MESSAGES or LANG environment variable selects it. With none set, the default is
// the user locale on macOS, iOS and Windows, the system locale on Android, or the
// system locale configuration on Linux. Other systems and Android builds without Cgo
// use the locale environment variables only.
// Browser builds use navigator.language. If the host locale is unavailable, C or POSIX,
// the default is en-US.
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
