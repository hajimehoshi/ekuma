// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "normalize of long runs of combining marks, also across the chunks of a long string"
flags: [noStrict]
---*/

var below = "̖", acute = "́", x = "x".repeat(65500);
var a = "e" + (below + acute).repeat(20), b = x + "e" + (acute + below).repeat(100);
assert.sameValue(String([
	a.normalize("NFD") === "e" + below.repeat(20) + acute.repeat(20),
	a.normalize("NFC") === "é" + below.repeat(20) + acute.repeat(19),
	b.normalize("NFD") === x + "e" + below.repeat(100) + acute.repeat(100),
	b.normalize("NFKC") === x + "é" + below.repeat(100) + acute.repeat(99),
].join()), "true,true,true,true");
