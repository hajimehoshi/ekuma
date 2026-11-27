// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Concatenations with the same contents give strings that keep their contents through every later use"
---*/

// The pieces are made at run time, so that every concatenation below happens when the script runs.
function piece(s) { return (s + "!").slice(0, -1); }

var warm1 = piece("warm-up-"), warm2 = piece("pieces!!");
for (var i = 0; i < 2000; i++) {
	assert.sameValue(warm1 + warm2, "warm-up-pieces!!");
}

// The same contents from other pieces, through each kind of concatenation.
var a = piece("String for key "), tag = piece("0.123456789"), b = piece(" in leaf node");
var leaves = [];
for (var i = 0; i < 32; i++) {
	leaves.push(a + tag + b, "String for key " + tag + b, `String for key ${tag} in leaf node`,
		a.concat(tag, b), (a + tag) + b, a + (tag + b));
}
for (var i = 0; i < leaves.length; i++) {
	assert.sameValue(leaves[i], "String for key 0.123456789 in leaf node");
	assert.sameValue(leaves[i].length, 39);
}

// Appending to a result, or prepending to it, past the point where a builder takes over, leaves it as it was.
var s = piece("0123456789") + piece("abcdefghij");
s = s + piece("ABCDEFGHIJ");
s = s + piece("klm");
var t1 = s + "x", t2 = s + "y", u1 = "x" + s, u2 = "y" + s;
for (var i = 0; i < 3; i++) {
	t1 += "x";
	t2 += "y";
	u1 = "x" + u1;
	u2 = "y" + u2;
}
assert.sameValue(s, "0123456789abcdefghijABCDEFGHIJklm");
assert.sameValue(t1, s + "xxxx");
assert.sameValue(t2, s + "yyyy");
assert.sameValue(u1, "xxxx" + s);
assert.sameValue(u2, "yyyy" + s);
assert.sameValue(piece("0123456789abcdefghij") + piece("ABCDEFGHIJklm"), s);

// A result used as a property key or an array index, then made again.
var o = {};
o[piece("property-") + piece("key-one")] = 1;
assert.sameValue(o[piece("property-key") + piece("-one")], 1);
assert.sameValue(Object.keys(o)[0], "property-key-one");
var arr = [];
arr[piece("12345") + piece("6789")] = "x";
assert.sameValue(arr.length, 123456790);
assert.sameValue(arr[piece("1234") + piece("56789")], "x");
var notIndex = piece("01234") + piece("56789");
arr[notIndex] = "y";
assert.sameValue(arr.length, 123456790);
assert.sameValue(arr["0123456789"], "y");
var m = new Map([[piece("map-key-") + piece("one"), 1]]);
assert.sameValue(m.get(piece("map-") + piece("key-one")), 1);

// Results that are not ASCII, read at positions in turn.
var e = piece("ééééé") + piece("日本語のテキスト");
for (var i = 0; i < 3; i++) {
	var f = piece("éééééé日本") + piece("語のテキスト");
	assert.sameValue(f, e.slice(0, 5) + "é" + e.slice(5));
	assert.sameValue(f.length, 14);
	assert.sameValue(f.charCodeAt(13), 0x30c8);
	assert.sameValue(f.charCodeAt(0), 0xe9);
	assert.sameValue(f.indexOf("テ"), 10);
	assert.sameValue(f.at(-1), "ト");
}

// A lone surrogate at the end of a result still joins the one that follows it.
var high = piece("abcdefgh") + "\ud83d";
assert.sameValue(high.length, 9);
var joined = high + "\ude00";
assert.sameValue(joined, "abcdefgh\u{1f600}");
assert.sameValue(joined.length, 10);
assert.sameValue(piece("abcdefgh") + "\ud83d", high);
assert.sameValue(high.length, 9);
assert.sameValue(high.charCodeAt(8), 0xd83d);

// Operands converted by user code.
var calls = 0;
var obj = { toString() { calls++; return "String for key "; } };
for (var i = 0; i < 3; i++) {
	assert.sameValue(obj + tag + b, leaves[0]);
	assert.sameValue(`${obj}${tag} in leaf node`, leaves[0]);
}
assert.sameValue(calls, 6);
