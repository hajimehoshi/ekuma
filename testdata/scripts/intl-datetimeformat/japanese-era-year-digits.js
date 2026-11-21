// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Japanese era years in full date styles use the requested numbering system"
---*/

const format = new Intl.DateTimeFormat("ja-JP-u-ca-japanese-nu-arab", {
	dateStyle: "full",
	timeZone: "UTC",
});
assert.sameValue(format.format(Date.UTC(2019, 4, 1)), "令和元年٥月١日水曜日");
assert.sameValue(format.format(Date.UTC(2024, 4, 10)), "令和٦年٥月١٠日金曜日");
