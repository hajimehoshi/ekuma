// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a month and year range stays shared across a week-year boundary within December"
---*/

const format = new Intl.DateTimeFormat("te-u-ca-buddhist", {
	year: "numeric",
	month: "long",
	timeZone: "UTC",
});
const start = Date.UTC(2025, 11, 27);
const end = Date.UTC(2025, 11, 28);
assert.sameValue(format.formatToParts(end).find(p => p.type === "year").value, "2026");

const parts = format.formatRangeToParts(start, end);
assert.sameValue(parts.every(p => p.source === "shared"), true);
assert.sameValue(parts.find(p => p.type === "year").value, "2025");
