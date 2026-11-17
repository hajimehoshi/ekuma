// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a lone surrogate in the source joins an escaped surrogate into a pair"
flags: [noStrict]
---*/

var r = [
	eval("'\\uD83D" + "\uDE00'") === "\u{1F600}",
	eval("'\uD83D" + "\\uDE00'") === "\u{1F600}",
];
try {
	eval("var a\uD83D;");
} catch (e) {
	r.push(e instanceof SyntaxError);
}
assert.sameValue(String(r.join()), "true,true,true");
