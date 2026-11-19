// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "searches for a needle that collides with every window of a rolling hash finish in linear time"
flags: [onlyStrict]
---*/

// A Thue-Morse sequence over "a" and "c" after a run of "b" has the hash of a run of "b" in the
// rolling hash of the Go standard library, so a rolling-hash search compares it at every window.
var thueMorse = "";
for (var i = 0; i < 128; i++) {
	var parity = 0;
	for (var x = i; x; x >>= 1) {
		parity ^= x & 1;
	}
	thueMorse += parity ? "c" : "a";
}
var needle = "b".repeat(2 ** 16 - 128) + thueMorse;
var s = "b".repeat(2 ** 22);
var nonASCII = "é" + s;

assert.sameValue(s.indexOf(needle), -1, "indexOf");
assert.sameValue(s.includes(needle), false, "includes");
assert.sameValue(s.lastIndexOf(needle), -1, "lastIndexOf");
assert.sameValue(s.split(needle).length, 1, "split");
assert.sameValue(s.replace(needle, "x").length, s.length, "replace");
assert.sameValue(s.replaceAll(needle, "x").length, s.length, "replaceAll");
assert.sameValue(nonASCII.indexOf(needle), -1, "indexOf in a non-ASCII string");
assert.sameValue(nonASCII.lastIndexOf(needle), -1, "lastIndexOf in a non-ASCII string");
assert.sameValue(new RegExp(needle).test(s), false, "RegExp test");
assert.sameValue(("\uDC00" + s).indexOf("\uDC00" + needle), -1, "indexOf of a needle that starts with a lone surrogate");

var found = s + needle + "b";
assert.sameValue(found.indexOf(needle), s.length, "indexOf of a needle at the end");
assert.sameValue(found.lastIndexOf(needle), s.length, "lastIndexOf of a needle at the end");
