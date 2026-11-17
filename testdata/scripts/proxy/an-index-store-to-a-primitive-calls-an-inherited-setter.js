// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an index store to a primitive calls an inherited setter"
flags: [noStrict]
---*/

var log = "";
Object.defineProperty(Object.prototype, "5", { set(v) { log += "setter:" + v; }, configurable: true });
(1)[5] = "a";
"abcdefg"[5] = "b";
delete Object.prototype[5];
assert.sameValue(String(log), "setter:a");
