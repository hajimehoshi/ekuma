// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A bound instanceof chain looks up each method inside the calls already selected"
---*/

const foreign = $262.createRealm().global;
const localMethod = Function.prototype[Symbol.hasInstance];
const foreignMethod = foreign.Function.prototype[Symbol.hasInstance];
const target = () => {};
const middle = target.bind();
const outer = middle.bind();
const inherited = outer.bind().bind();
const log = [];

function depth() {
	return new Error().stack.split("\n").filter(line => line.includes("[Symbol.hasInstance]")).length;
}
function select(receiver, name, method) {
	Object.defineProperty(receiver, Symbol.hasInstance, {
		get() {
			assert.sameValue(this, receiver);
			log.push(name + depth());
			return method;
		},
	});
}

select(outer, "outer", localMethod);
select(middle, "middle", null);
select(target, "target", foreignMethod);
Object.defineProperty(target, "prototype", {
	get() {
		assert.sameValue(this, target);
		log.push("prototype" + depth());
		return null;
	},
});

let caught;
try {
	({}) instanceof inherited;
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof foreign.TypeError, true);
assert.sameValue(log.join(), "outer2,middle3,target3,prototype4");
