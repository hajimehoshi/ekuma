// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "hebrew date fields"
flags: [noStrict]
---*/

const d = Temporal.PlainDate.from("2024-03-15[u-ca=hebrew]");
assert.sameValue(String([d.era, d.eraYear, d.year, d.monthCode, d.day, d.weekOfYear, d.monthsInYear].join(" ")), "am 5784 5784 M06 5  13");
