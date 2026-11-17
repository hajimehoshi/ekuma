// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "stringify called from toJSON"
flags: [noStrict]
---*/

assert.sameValue(String(JSON.stringify({a: {toJSON: function() { return JSON.stringify([1, "b"]); }}, c: "d"})), "{\"a\":\"[1,\\\"b\\\"]\",\"c\":\"d\"}");
