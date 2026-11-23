// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Cached numeric property accesses distinguish typed arrays from ordinary objects with the same prototype"
---*/

function read(object) {
	return object["-1"];
}

function check(view) {
	const proto = Object.getPrototypeOf(view);
	proto["-1"] = 42;
	assert.sameValue(read(Object.create(proto)), 42);
	assert.sameValue(read(view), undefined);
}

const view = new Uint8Array(1);
check(view);
Object.setPrototypeOf(view, {});
check(view);

function store(object, value) {
	object["-1"] = value;
}

const calls = [];
const proto = {set "-1"(value) { calls.push("setter"); }};
Object.setPrototypeOf(view, proto);
const value = {valueOf() { calls.push("convert"); return 1; }};
store(Object.create(proto), value);
store(view, value);
const child = Object.create(view);
store(child, value);
assert.sameValue(calls.join(), "setter,convert");
assert.sameValue(Object.hasOwn(child, "-1"), false);
