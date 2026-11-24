// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: A super call in an async arrow passes its arguments and new.target
flags: [async]
---*/

class Base {
	constructor(a, b) {
		this.value = a - b;
		this.target = new.target;
	}
}
class Derived extends Base {
	constructor(x) {
		return (async () => {
			await 0;
			return super(x, 3);
		})();
	}
}

new Derived(17).then(instance => {
	assert.sameValue(instance.value, 14);
	assert.sameValue(instance.target, Derived);
}).then(() => $DONE(), $DONE);
