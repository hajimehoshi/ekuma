// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "base64 growth during options"
flags: [noStrict]
---*/

const buffer = new SharedArrayBuffer(2, {maxByteLength: 8});
const tracking = new Uint8Array(buffer, 1);
const fixed = new Uint8Array(buffer, 1, 1);
const result = tracking.setFromBase64("AQID", {
	get lastChunkHandling() { buffer.grow(4); return "loose"; }
});
const limited = fixed.setFromBase64("BA==");
assert.sameValue(String([result.read, result.written, limited.read, limited.written,
	new Uint8Array(buffer).join()].join(";")), "4;3;4;1;0,4,2,3");
