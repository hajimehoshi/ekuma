// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a counter of a[++i] or a[i++] that is not a number is converted once, before the read, and a conversion that throws leaves it and reads nothing"
---*/

var reads = [];
var a = new Proxy([10, 20, 30, 40], {
	get(t, k) { reads.push(String(k)); return t[k]; },
});
function prefix(i) { var v = a[++i]; return [v, i]; }
function postfix(i) { var v = a[i++]; return [v, i]; }
function show(r) { return r.map(function (x) { return typeof x === "bigint" ? x + "n" : String(x); }).join(" "); }

var conversions = 0;
var counter = { valueOf() { conversions++; return 1; } };
assert.sameValue(show(prefix(counter)), "30 2");
assert.sameValue(show(postfix(counter)), "20 2");
assert.sameValue(conversions, 2);

// A BigInt counter steps as a BigInt, and the key is its string.
assert.sameValue(show(prefix(0n)), "20 1n");
assert.sameValue(show(postfix(2n)), "30 3n");
assert.sameValue(reads.join(), "2,1,1,2");

// A conversion that throws reads nothing, and the counter keeps its value.
var bad = { valueOf() { throw new RangeError("no"); } };
function prefixKeeps(i) { try { a[++i]; } catch (e) { return [e.constructor.name, i === bad]; } }
function postfixKeeps(i) { try { a[i++]; } catch (e) { return [e.constructor.name, i === bad]; } }
reads = [];
assert.sameValue(show(prefixKeeps(bad)), "RangeError true");
assert.sameValue(show(postfixKeeps(bad)), "RangeError true");
assert.sameValue(reads.length, 0);
