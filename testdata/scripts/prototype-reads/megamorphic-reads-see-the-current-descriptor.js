// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "megamorphic reads observe a shadowing own property and a replacement inherited getter"
---*/

const base = { x: 100 };
const nodes = [];
for (let i = 0; i < 4; i++) {
	const node = Object.create(Object.create(base));
	node.index = i;
	nodes.push(node);
}
const dictionary = Object.create(base);
dictionary.index = 4;
dictionary.temporary = true;
delete dictionary.temporary;

function read(node) { return node.x; }
for (let round = 0; round < 2; round++) {
	read(dictionary);
	for (const node of nodes) read(node);
}

dictionary.x = 42;
assert.sameValue(read(dictionary), 42);

Object.defineProperty(base, "x", { get() { return this.index; } });
for (let i = 0; i < nodes.length; i++) {
	assert.sameValue(read(nodes[i]), i);
}
