// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "plain calls pass undefined as this after a method call"
flags: [noStrict]
---*/

function g() {
	"use strict";
	return this;
}

// A call of m leaves o in the registers of its this value and its arguments.
var o = {
	m(a, b, c, d) {
	},
};

assert.sameValue((function (o) {
	o.m(o, o, o, o);
	return g();
})(o), undefined, "a call of a variable");

assert.sameValue((function (o) {
	o.m(o, o, o, o);
	return (0, g)();
})(o), undefined, "a call of an expression");

assert.sameValue((function (o) {
	"use strict";
	o.m(o, o, o, o);
	return g();
})(o), undefined, "a tail call");

assert.sameValue((function (o) {
	o.m(o, o, o, o);
	return g``;
})(o), undefined, "a tagged template");

assert.sameValue((function (o) {
	o.m(o, o, o, o);
	return g(...[]);
})(o), undefined, "a spread call");

assert.sameValue((function (o) {
	var eval = g;
	o.m(o, o, o, o);
	return eval();
})(o), undefined, "a call of a variable named eval");
