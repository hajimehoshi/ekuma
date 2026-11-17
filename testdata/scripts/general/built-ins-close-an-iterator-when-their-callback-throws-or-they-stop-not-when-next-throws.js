// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "built-ins close an iterator when their callback throws or they stop, not when next throws"
flags: [noStrict]
---*/

var log = [];
function mk(nextThrows) { return { [Symbol.iterator]() { var i = 0; return { next() { if (nextThrows && i == 1) throw "next"; i++; return { done: i > 4, value: i }; }, return() { log.push("return"); throw "ret"; } }; } }; }
var add = Set.prototype.add;
Set.prototype.add = function (v) { if (v == 2) throw "add"; return add.call(this, v); };
try { new Set(mk(false)); } catch (e) { log.push(e); }
try { new Set(mk(true)); } catch (e) { log.push(e); }
Set.prototype.add = add;
var other = { size: 1, has() {}, keys() { return mk(false)[Symbol.iterator](); } };
try { new Set([1, 2, 3]).isSupersetOf(other); } catch (e) { log.push(e); }
assert.sameValue(String(log.join()), "return,add,next,return,ret");
