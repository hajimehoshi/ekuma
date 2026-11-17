// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "base64 shrink during options"
flags: [noStrict]
---*/

const buffer = new ArrayBuffer(4, {maxByteLength: 8});
const dst = new Uint8Array(buffer, 1, 3);
const calls = [];
try {
	dst.setFromBase64("AQID", {
		get alphabet() { calls.push("alphabet"); buffer.resize(2); return "base64"; },
		get lastChunkHandling() { calls.push("last"); return "loose"; }
	});
} catch (e) { calls.push(e instanceof TypeError); }
assert.sameValue(String(calls.join()), "alphabet,last,true");
