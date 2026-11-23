// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: A caller reads its updated local after a global setter grows the stack
---*/

function noop() {}
function readAfterWrite() {
	let observed;
	Object.defineProperty(globalThis, "throughSetter", {
		set(value) {
			noop(...Array(8192));
			observed = value;
		}
	});
	throughSetter = 42;
	return observed;
}

assert.sameValue(readAfterWrite(), 42);
