// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "searches for negative and out-of-range numbers"
flags: [noStrict]
---*/

assert.sameValue(String([Int8Array, Int16Array, Int32Array, Uint8Array, Uint16Array, Uint8ClampedArray, Float32Array].map(C => {
	var a = new C([5, -1, 255, 65535, -129, 0.5, 7, -1, 0]);
	return [a.indexOf(-1), a.lastIndexOf(-1), a.includes(255), a.indexOf(65535), a.indexOf(-129), a.indexOf(0.5), a.lastIndexOf(0), a.indexOf(-0)].join();
}).join(" ")), "1,7,false,-1,-1,-1,8,5 1,7,true,-1,4,-1,8,5 1,7,true,3,4,-1,8,5 -1,-1,true,-1,-1,-1,8,5 -1,-1,true,1,-1,-1,8,5 -1,-1,true,-1,-1,-1,8,1 1,7,true,3,4,5,8,8");
