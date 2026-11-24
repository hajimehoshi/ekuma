// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A catch inside a loop cannot intercept an exception from closing that loop"
---*/

const marker = {};
function* source() {
	try { yield 1; } finally { throw marker; }
}
function* values() {
	for (const x of source()) {
		try {
			yield x;
			break;
		} catch (e) {
			yield "caught inside";
		}
	}
}
const gen = values();
assert.sameValue(gen.next().value, 1);
let caught;
try { gen.next(); } catch (e) { caught = e; }
assert.sameValue(caught, marker);
assert.sameValue(gen.next().done, true);
