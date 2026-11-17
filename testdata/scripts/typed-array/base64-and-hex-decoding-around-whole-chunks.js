// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "base64 and hex decoding around whole chunks"
flags: [noStrict]
---*/

function r(f) { try { return f(); } catch (e) { return e.name; } }
var a = new Uint8Array(4), b = new Uint8Array(8);
assert.sameValue(String([Uint8Array.fromBase64("AQID BAUG\nBw==").join(),
	Uint8Array.fromBase64("AQIDBA-_", {alphabet: "base64url"}).join(),
	JSON.stringify(a.setFromBase64("AQIDBAUG")), a.join(),
	r(() => b.setFromBase64("AQIDBAUG*AAA")), b.join(),
	Uint8Array.fromBase64("AQIDBA", {lastChunkHandling: "stop-before-partial"}).join(),
	Uint8Array.fromHex("00ff10Ab").join(),
	JSON.stringify(a.setFromHex("0102030405")), r(() => a.setFromHex("0102zz")), a.join()].join(" ")), "1,2,3,4,5,6,7 1,2,3,4,15,191 {\"read\":4,\"written\":3} 1,2,3,0 SyntaxError 1,2,3,4,5,6,0,0 1,2,3 0,255,16,171 {\"read\":8,\"written\":4} SyntaxError 1,2,3,4");
