// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "random access to a long non-ASCII string with pairs across every 64th code unit"
flags: [noStrict]
---*/

var s = "bc", u = [0x62, 0x63];
for (var i = 0; i < 300; i++) { s += "a\u{1F600}日"; u.push(0x61, 0xD83D, 0xDE00, 0x65E5); }
s += "\uD800"; u.push(0xD800);
var n = s.length, bad = 0;
for (var i = 0; i < n; i++) {
	var j = (i * 7919) % n;
	if (s.charCodeAt(j) !== u[j] || s.slice(j, j + 1).charCodeAt(0) !== u[j]) bad++;
}
for (var k = n; k >= 0; k -= 61) {
	var want = -1;
	for (var j = Math.min(k, n - 2); j >= 0 && want < 0; j--) if (u[j] === 0x61 && u[j + 1] === 0xD83D) want = j;
	if (s.lastIndexOf("a\u{1F600}", k) !== want) bad++;
}
assert.sameValue(String([n, bad, s.lastIndexOf("日"), s.lastIndexOf("日\uD800")].join(" ")), "1203 0 1201 1201");
