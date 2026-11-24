// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "exec assigned after a new prototype property"
flags: [noStrict, noRepeat]
noRepeatReason: "it starts with the original RegExp.prototype.exec"
---*/

// The built-ins see an exec assigned after RegExp.prototype got new properties, which may move
// the storage of its properties.
var log = [];
var warm = ["abab".replace(/b/g, "x"), /b/.test("ab")];
for (var i = 0; i < 20; i++) RegExp.prototype["extra" + i] = i;
var again = ["abab".replace(/b/g, "x"), /b/.test("ab")];
RegExp.prototype.exec = function (s) { log.push("exec"); return null; };
assert.sameValue(String([warm.join(), again.join(), "abab".replace(/b/g, "x"), /b/.test("ab"), log.length].join(" ")), "axax,true axax,true abab false 2");
