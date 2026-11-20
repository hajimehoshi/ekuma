// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Destructuring keeps the assigned source for later properties and the expression result"
---*/

function readLastProperty() {
	let x, y;
	({a: x, b: y} = (x = {a: 3, b: 4}));
	return y;
}

function assignFirstElement(source) {
	let x;
	return ([x] = (x = source));
}

assert.sameValue(readLastProperty(), 4);
const source = [3];
assert.sameValue(assignFirstElement(source), source);
