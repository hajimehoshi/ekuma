// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "properties added to built-in objects"
flags: [noStrict, noRepeat]
noRepeatReason: "it counts the properties that it adds to built-in objects"
---*/

var out = [], before = Object.getOwnPropertyNames(Math).length;
Math.extra = 1;
var names = Object.getOwnPropertyNames(Math);
var s = 0;
for (var i = 0; i < 3; i++) s += Math.abs(-1) + Math.extra;
out.push(names.length - before, names[names.length - 1], Object.keys(Math).join(), s);
for (var i = 0; i < 9; i++) Boolean["p" + i] = i;
out.push(Object.getOwnPropertyNames(Boolean).slice(0, 4).join(), Boolean.length, Boolean.p8, Object.keys(Boolean).length);
delete Boolean.p0;
Object.defineProperty(Math, "abs", {enumerable: true});
out.push(Object.getOwnPropertyNames(Boolean).length, Boolean.name, Object.keys(Math).join());
assert.sameValue(String(out.join("|")), "1|extra|extra|6|length,name,prototype,p0|1|8|9|11|Boolean|abs,extra");
