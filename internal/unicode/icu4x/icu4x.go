// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package icu4x holds the year tables of the Chinese, Korean and Umm al-Qura calendars that
// Temporal uses, generated from a release of ICU4X's icu_calendar crate.
//
// The tables are under the Unicode License v3, the license of the ICU4X data, which is in the
// file LICENSE of this directory. That license covers this package only.
package icu4x
