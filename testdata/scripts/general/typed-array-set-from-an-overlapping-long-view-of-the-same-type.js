// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "typed array set from an overlapping long view of the same type"
flags: [noStrict]
---*/

var a = new Uint8Array(70000);
for (var i = 0; i < a.length; i++) a[i] = i & 0xff;
a.set(a.subarray(0, 69999), 1);
assert.sameValue(String([a[0], a[1], a[65536], a[65537], a[69999]].join()), "0,0,255,0,110");
