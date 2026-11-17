// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "little-endian"
flags: [noStrict]
---*/

// The specification leaves the byte order of typed arrays to the platform.
assert.sameValue(String([
	new Uint8Array(new Uint16Array([0x0102]).buffer),
	new Uint8Array(new Int32Array([-2]).buffer),
	new Uint8Array(new Float32Array([1.5]).buffer),
	new Uint8Array(new Float64Array([-2]).buffer),
	new Uint8Array(new Float16Array([1.5]).buffer),
	new Uint8Array(new BigInt64Array([-0x0102n]).buffer),
].join(" ")), "2,1 254,255,255,255 0,0,192,63 0,0,0,0,0,0,0,192 0,62 254,254,255,255,255,255,255,255");
