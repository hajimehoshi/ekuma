// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "stringify sees changes that its calls make to later objects"
flags: [noStrict]
---*/

var r1 = {v: 0}, r2 = {v: 1};
var a = JSON.stringify([r1, {toJSON: function() { Object.defineProperty(r2, "v", {get: function() { return "g"; }, enumerable: true}); return 0; }}, r2]);
var b = JSON.stringify({a: {}, b: {get x() { Object.prototype.toJSON = function() { return "p"; }; return 1; }}, c: {}});
delete Object.prototype.toJSON;
assert.sameValue(String(a + b), "[{\"v\":0},0,{\"v\":\"g\"}]{\"a\":{},\"b\":{\"x\":1},\"c\":\"p\"}");
