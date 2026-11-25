// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a promise resolve function resolves its promise after it became a key of a weak collection and a prototype"
flags: [async]
---*/

var resolve;
var p = new Promise(r => { resolve = r; });
var m = new WeakMap();
m.set(resolve, 1);
Object.create(resolve);
resolve(2);
p.then(v => {
	assert.sameValue(v, 2);
	assert.sameValue(m.get(resolve), 1);
}).then(() => $DONE(), $DONE);
