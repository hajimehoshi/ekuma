// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// SetTimeZone sets the time zone of local time in vm to loc.
func SetTimeZone(vm *VM, loc *time.Location) {
	vm.e.SetDefaultTimeZone(datetime.LocationZone(loc))
}
