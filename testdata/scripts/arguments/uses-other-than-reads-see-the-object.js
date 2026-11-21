// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Uses of arguments other than reads of its length and elements, next to such reads, see the arguments object"
---*/

function isArguments(v) {
	return Object.prototype.toString.call(v) === "[object Arguments]";
}
function self() {
	return this;
}

function method() {
	return [arguments.length, isArguments(arguments[0]())].join();
}
assert.sameValue(method(self), "1,true");

function tag() {
	return [arguments.length, isArguments(arguments[0]``)].join();
}
assert.sameValue(tag(self), "1,true");

function assign() {
	arguments[0] += 2;
	return arguments[0];
}
assert.sameValue(assign(1), 3);

function update() {
	arguments[0]++;
	return arguments[0];
}
assert.sameValue(update(1), 2);

function destructure() {
	[arguments[0]] = [2];
	return arguments[0];
}
assert.sameValue(destructure(1), 2);

function forIn() {
	for (arguments[0] in { k: 1 });
	return arguments[0];
}
assert.sameValue(forIn(1), "k");

function forOf() {
	for (arguments[0] of [2]);
	return arguments[0];
}
assert.sameValue(forOf(1), 2);

function remove() {
	return [delete arguments[0], arguments[0]].join();
}
assert.sameValue(remove(1), "true,");

function optional() {
	return [arguments[0], arguments?.[1]].join();
}
assert.sameValue(optional(1, 2), "1,2");

function arrow() {
	return [arguments[0], (() => arguments[1])()].join();
}
assert.sameValue(arrow(1, 2), "1,2");

function* generator() {
	yield [arguments.length, arguments[1]].join();
}
assert.sameValue(generator(1, 2).next().value, "2,2");
