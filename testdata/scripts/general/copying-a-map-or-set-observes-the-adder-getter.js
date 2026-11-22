// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Copying a Map or Set observes changes to the destination made by the adder getter"
---*/

for (const [Collection, adder] of [[Map, "set"], [Set, "add"]]) {
	const source = new Collection();
	const proto = Collection.prototype;
	const original = Object.getOwnPropertyDescriptor(proto, adder);
	Object.defineProperty(proto, adder, {
		configurable: true,
		get() {
			Object.defineProperty(proto, adder, original);
			original.value.call(this, "temporary", 1);
			this.delete("temporary");
			return original.value;
		},
	});
	assert.sameValue(new Collection(source).size, 0, Collection.name);
}
