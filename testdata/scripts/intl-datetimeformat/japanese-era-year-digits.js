// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Japanese year patterns use 元 for the first era year and localized digits for later years"
---*/

const format = new Intl.DateTimeFormat("ja-JP-u-ca-japanese-nu-arab", {
	dateStyle: "full",
	timeZone: "UTC",
});
assert.sameValue(format.format(Date.UTC(2019, 4, 1)), "令和元年٥月١日水曜日");
assert.sameValue(format.format(Date.UTC(2024, 4, 10)), "令和٦年٥月١٠日金曜日");

const year = new Intl.DateTimeFormat("ja-JP-u-ca-japanese", {
	year: "numeric",
	timeZone: "UTC",
});
assert.sameValue(year.format(Date.UTC(2019, 4, 1)), "令和元年");
