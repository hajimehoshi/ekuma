// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a compact pattern without digits supplies the entire magnitude"
---*/

const format = new Intl.NumberFormat("it", {notation: "compact", compactDisplay: "long"});
assert.sameValue(format.format(1000), "mille");

const parts = format.formatToParts(1000);
assert.sameValue(parts.length, 1);
assert.sameValue(parts[0].type, "compact");
assert.sameValue(parts[0].value, "mille");

assert.sameValue(format.format(-1000), "-mille");
assert.sameValue(format.formatRange(1000, 2000), "mille - 2 mila");
