// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Global reads and writes still reach the global object after its temporary properties are deleted"
---*/

const realm = $262.createRealm();
const result = realm.evalScript(`
	(function () {
		function read() { return entry; }
		function write(value) { entry = value; }
		globalThis.entry = 1;
		write(read());
		for (let i = 0; i < 32; i++) globalThis["temp" + i] = i;
		for (let i = 31; i >= 0; i--) delete globalThis["temp" + i];
		globalThis.entry = 2;
		const observed = read();
		write(3);
		return [observed, globalThis.entry];
	})();
`);
assert.sameValue(result[0], 2);
assert.sameValue(result[1], 3);
