// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a Map or Set made from an array longer than the room it makes ahead keeps every key in order"
flags: [noStrict]
---*/

// key returns the key i of a sequence of numbers, long strings and objects.
var objects = [];
function key(i) {
	switch (i % 3) {
	case 0:
		return i;
	case 1:
		return "a key longer than sixteen bytes " + i;
	}
	return objects[i] || (objects[i] = {i: i});
}

// check makes a Set and a Map from the keys at the indices order and compares them with the
// keys 0 to n-1 in order.
function check(order, n, name) {
	var keys = order.map(key);
	var s = new Set(keys), m = new Map(order.map(function (i) { return [key(i), i]; }));
	assert.sameValue(s.size, n, name + ": Set size");
	assert.sameValue(m.size, n, name + ": Map size");
	var j = 0, ok = true;
	for (var k of s) {
		if (k !== key(j++)) ok = false;
	}
	j = 0;
	for (var [k, v] of m) {
		if (k !== key(j) || v !== j) ok = false;
		j++;
	}
	for (var i = 0; i < n; i++) {
		if (!s.has(key(i)) || m.get(key(i)) !== i) ok = false;
	}
	assert.sameValue(ok && !s.has(key(n)) && !m.has(key(n)), true, name + ": every key in order");
}

// Every key is new, then some repeat, in an array long enough to get room for all of them.
var n = 140000, order = [];
for (var i = 0; i < n; i++) order.push(i);
for (var i = 0; i < 1000; i++) order.push(i);
check(order, n, "distinct");

// A key repeats early, so the collection grows past the room it made ahead.
n = 70000;
order = [];
for (var i = 0; i < 20; i++) order.push(i);
order.push(0);
for (var i = 20; i < n; i++) order.push(i);
check(order, n, "early repeat");
