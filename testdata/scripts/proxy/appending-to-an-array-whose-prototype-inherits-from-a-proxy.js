// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "appending to an array whose prototype inherits from a proxy"
flags: [noStrict]
---*/

var log = "";
var arr = [1, 2];
arr[2] = 3;
Object.setPrototypeOf(Array.prototype, new Proxy({}, { set(t, k, v, r) { log += "set:" + k; return true; } }));
arr[3] = 4;
Object.setPrototypeOf(Array.prototype, Object.prototype);
assert.sameValue(String(log + " " + arr.length), "set:3 3");
