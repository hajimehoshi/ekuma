// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// The test binary cannot capture the output of an example in a browser.
//go:build !js

package intl_test

import (
	"context"
	"fmt"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

func Example_numberFormat() {
	var vm ekuma.VM
	s, err := vm.ParseScript("price.js", `
		const usd = new Intl.NumberFormat("en-US", {
			style: "currency",
			currency: "USD",
		});
		usd.format(1234.5)
	`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output: $1,234.50
}

func Example_defaultLocale() {
	var vm ekuma.VM
	if err := vm.SetLocale("de-DE"); err != nil {
		panic(err)
	}
	s, err := vm.ParseScript("locale.js", `(1234.5).toLocaleString()`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output: 1.234,5
}

func Example_dateTimeFormat() {
	var vm ekuma.VM
	s, err := vm.ParseScript("date.js", `
		const format = new Intl.DateTimeFormat("en-GB", {
			timeZone: "UTC",
			year: "numeric",
			month: "long",
			day: "numeric",
		});
		format.format(new Date("2024-01-02T00:00:00Z"))
	`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output: 2 January 2024
}

// This example formats the last day of Heisei and the first day of Reiwa.
func Example_japaneseCalendar() {
	var vm ekuma.VM
	s, err := vm.ParseScript("eras.js", `
		const format = new Intl.DateTimeFormat("ja-JP", {
			calendar: "japanese",
			dateStyle: "full",
			timeZone: "UTC",
		});
		["2019-04-30", "2019-05-01"]
			.map((date) => format.format(new Date(date)))
			.join("\n")
	`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output:
	// 平成31年4月30日火曜日
	// 令和元年5月1日水曜日
}

func Example_collator() {
	var vm ekuma.VM
	s, err := vm.ParseScript("cities.js", `
		const collator = new Intl.Collator("sv");
		["Örebro", "Ängelholm", "Alingsås", "Åmål"].sort(collator.compare).join(", ")
	`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output: Alingsås, Åmål, Ängelholm, Örebro
}

func Example_segmenter() {
	var vm ekuma.VM
	s, err := vm.ParseScript("graphemes.js", `
		const segmenter = new Intl.Segmenter("en", { granularity: "grapheme" });
		Array.from(segmenter.segment("A🇯🇵B"), ({ segment }) => segment).join(" | ")
	`)
	if err != nil {
		panic(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(v.String())

	// Output: A | 🇯🇵 | B
}
