// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "cached array iterator acquisition observes writes to the iterator methods"
---*/

function collect(a) {
	var [...rest] = a;
	return rest;
}
var source = [1, 2];
collect(source);
collect(source);

var iteratorProto = Object.getPrototypeOf(source.values());
var next = iteratorProto.next;
iteratorProto.next = function () { return {done: true}; };
var empty = collect(source);
iteratorProto.next = next;
assert.sameValue(empty.length, 0);

var values = Array.prototype[Symbol.iterator];
Array.prototype[Symbol.iterator] = function* () { yield 3; };
var replaced = collect(source);
Array.prototype[Symbol.iterator] = values;
assert.sameValue(replaced.join(), "3");
