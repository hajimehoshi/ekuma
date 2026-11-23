// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a metazone takes the long standard name that CLDR's root aliases give it"
---*/

function zoneName(locale, timeZone, date) {
	const format = new Intl.DateTimeFormat(locale, {
		hour: "numeric",
		timeZone,
		timeZoneName: "long",
	});
	return format.formatToParts(date).find(p => p.type === "timeZoneName").value;
}

const winter = Date.UTC(2024, 0, 15, 12);
assert.sameValue(zoneName("en-GB", "Europe/London", winter), "Greenwich Mean Time");
assert.sameValue(zoneName("en-GB", "Europe/Dublin", winter), "Greenwich Mean Time");
assert.sameValue(zoneName("ja", "Europe/London", winter), "グリニッジ標準時");
