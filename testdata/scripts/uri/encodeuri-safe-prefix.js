// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "URI encoding and lone-surrogate errors after a long safe prefix"
flags: [noStrict]
---*/

const encode = encodeURI;
const prefix = "abcXYZ09-_.!~*'()".repeat(128);
if (encode(prefix + "😀 x") !== prefix + "%F0%9F%98%80%20x") {
	throw new Error("incorrect encoding after safe prefix");
}
for (const suffix of ["\uD800", "\uDC00"]) {
	let caught = false;
	try { encode(prefix + suffix); }
	catch (e) {
		if (!(e instanceof URIError)) throw e;
		caught = true;
	}
	if (!caught) throw new Error("missing URIError after safe prefix");
}
