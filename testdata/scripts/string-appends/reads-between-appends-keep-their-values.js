// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Values read from a variable between appends to it keep their contents"
---*/

// The strings grow past the length from which concatenations append to a builder.
var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function viaVariable() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		var s = r;
		kept.push(s);
	}
	return kept;
}

function viaArgument() {
	var r = long, kept = [];
	function keep(v) { kept.push(v); }
	for (var i = 0; i < 40; i++) {
		r += "x";
		keep(r);
	}
	return kept;
}

function viaProperty() {
	var r = long, o = {}, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		o["k" + i] = r;
	}
	for (var i = 0; i < 40; i++) {
		kept.push(o["k" + i]);
	}
	return kept;
}

function viaElement() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept[i] = r;
	}
	return kept;
}

function viaArrayLiteral() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push([r][0]);
	}
	return kept;
}

function viaTemplate() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(`${r}`);
	}
	return kept;
}

function viaEmptyConcatenation() {
	var r = long, kept = [], empty = "";
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(r + empty);
		kept.pop();
		kept.push(empty + r);
	}
	return kept;
}

function viaMapKey() {
	var r = long, m = new Map(), kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		m.set(r, i);
	}
	m.forEach(function (v, k) { kept.push(k); });
	return kept;
}

function viaPropertyKey() {
	var r = long, o = {}, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		o[r] = i;
	}
	for (var k in o) {
		kept.push(k);
	}
	return kept;
}

var globalKept = [];
function viaGlobal() {
	var r = long;
	for (var i = 0; i < 40; i++) {
		r += "x";
		globalKept[i] = r;
	}
	return globalKept;
}

function viaReturn() {
	var r = long, kept = [];
	function grow(n) {
		var r = long;
		for (var i = 0; i < n; i++) {
			r += "x";
		}
		return r;
	}
	for (var i = 0; i < 40; i++) {
		kept.push(grow(i + 1));
	}
	return kept;
}

function viaCompoundResult() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		kept.push(r += "x");
	}
	return kept;
}

function viaSelf() {
	var r = long, kept = [];
	for (var i = 0; i < 4; i++) {
		r += "x";
		r += r;
		kept.push(r);
	}
	return kept;
}

// An append whose result is one of its operands.
function grown() {
	var s = long;
	for (var i = 0; i < 40; i++) {
		s += "y";
	}
	return s;
}
function sharedOperands() {
	var s = grown();
	var r = "";
	r += "";
	r += s;
	r += "x";
	var u = grown();
	var t = u;
	t += "";
	t += "x";
	return [s, r, u, t];
}

function check(name, kept, expected) {
	assert.sameValue(kept.length, expected.length, name + " count");
	for (var i = 0; i < expected.length; i++) {
		assert.sameValue(kept[i], expected[i], name + " " + i);
	}
}

var expected = [];
for (var i = 1; i <= 40; i++) {
	expected.push(long + "x".repeat(i));
}
check("variable", viaVariable(), expected);
check("argument", viaArgument(), expected);
check("property", viaProperty(), expected);
check("element", viaElement(), expected);
check("array literal", viaArrayLiteral(), expected);
check("template", viaTemplate(), expected);
check("empty concatenation", viaEmptyConcatenation(), expected);
check("map key", viaMapKey(), expected);
check("property key", viaPropertyKey(), expected);
check("global", viaGlobal(), expected);
check("return", viaReturn(), expected);
check("compound result", viaCompoundResult(), expected);

var doubled = [], d = long;
for (var i = 0; i < 4; i++) {
	d = d + "x";
	d = d + d;
	doubled.push(d);
}
check("self", viaSelf(), doubled);

var y40 = long + "y".repeat(40);
check("shared operands", sharedOperands(), [y40, y40 + "x", y40, y40 + "x"]);
