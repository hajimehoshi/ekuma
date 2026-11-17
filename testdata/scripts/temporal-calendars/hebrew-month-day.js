// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "hebrew month day"
flags: [noStrict]
---*/

// A month day string keeps the month code and the day of its date in the calendar.
assert.sameValue(String(Temporal.PlainMonthDay.from("2023-01-01[u-ca=hebrew]").toString()), "1972-12-13[u-ca=hebrew]");
