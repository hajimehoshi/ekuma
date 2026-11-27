// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Code that reads a variable without naming it in the function sees each value that appends give it, and keeps the values it read"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function check(name, kept) {
	assert.sameValue(kept.length, 40, name + " count");
	for (var i = 0; i < 40; i++) {
		assert.sameValue(kept[i], long + "x".repeat(i + 1), name + " " + i);
	}
}

function closure() {
	var r = long, kept = [];
	function get() { return r; }
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(get());
	}
	return kept;
}
check("closure", closure());

function argumentsObject(r) {
	var kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(arguments[0]);
	}
	return kept;
}
var fromArguments = argumentsObject(long);
// A strict function's arguments object keeps the argument it was called with.
if (fromArguments[0] === long) {
	for (var i = 0; i < 40; i++) {
		assert.sameValue(fromArguments[i], long, "unmapped arguments " + i);
	}
} else {
	check("mapped arguments", fromArguments);
}

function directEval() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(eval("r"));
	}
	return kept;
}
check("direct eval", directEval());
