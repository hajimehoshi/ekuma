// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Copying a Map or Set observes changes to the destination made by the adder getter"
---*/

for (const [Collection, adder] of [[Map, "set"], [Set, "add"]]) {
	const source = new Collection();
	const proto = Collection.prototype;
	const original = Object.getOwnPropertyDescriptor(proto, adder);

	// copy copies source while the getter for the adder runs change with the destination.
	function copy(change) {
		Object.defineProperty(proto, adder, {
			configurable: true,
			get() {
				Object.defineProperty(proto, adder, original);
				change(this);
				return original.value;
			},
		});
		return new Collection(source);
	}

	const emptied = copy(function (destination) {
		destination[adder]("temporary", 1);
		destination.delete("temporary");
	});
	assert.sameValue(emptied.size, 0, Collection.name);

	source[adder]("copied", 1);
	const filled = copy(function (destination) {
		destination[adder]("kept", 1);
	});
	assert.sameValue([...filled.keys()].join(), "kept,copied", Collection.name);
}
