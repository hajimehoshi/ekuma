// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !race

package ekuma_test

// raceEnabled reports whether the race detector is on, which changes allocation counts.
const raceEnabled = false
