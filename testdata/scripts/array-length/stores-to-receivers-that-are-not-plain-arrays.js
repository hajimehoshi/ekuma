// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Stores to the length of proxies, instances of Array subclasses, arrays with other prototypes or own properties, arrays of other realms and objects that are not arrays take the steps of their receivers, in sloppy and strict code on every iteration"
flags: [noStrict]
---*/

function setSloppy(o, v) {
	o.length = v;
}

function setStrict(o, v) {
	"use strict";
	o.length = v;
}

function thrown(f) {
	try {
		f();
	} catch (e) {
		return e.constructor;
	}
}

class Sub extends Array {}
var other = $262.createRealm().global;

for (var round = 0; round < 3; round++) {
	var target = [0, 1, 2];
	var log = [];
	var proxy = new Proxy(target, {
		set(t, k, v, r) {
			log.push(String(k) + "=" + v);
			return Reflect.set(t, k, v, r);
		},
	});
	setStrict(proxy, 1);
	setStrict(proxy, 0);
	assert.sameValue(log.join(), "length=1,length=0");
	assert.sameValue(target.length, 0);
	var refusing = new Proxy([0, 1], {
		set() {
			return false;
		},
	});
	setSloppy(refusing, 0);
	assert.sameValue(thrown(() => setStrict(refusing, 0)), TypeError);
	assert.sameValue(refusing.length, 2);

	var sub = Sub.of(0, 1, 2);
	setStrict(sub, 1);
	assert.sameValue(sub.join(), "0");
	assert.sameValue(sub instanceof Sub, true);

	var protoless = Object.setPrototypeOf([0, 1, 2], null);
	setStrict(protoless, 1);
	assert.sameValue(protoless.length, 1);
	assert.sameValue(1 in protoless, false);
	var named = [0, 1, 2];
	named.name = "named";
	setStrict(named, 0);
	assert.sameValue(named.length, 0);
	assert.sameValue(named.name, "named");

	var foreign = other.Array.of(0, 1, 2);
	setStrict(foreign, 1);
	assert.sameValue(foreign.join(), "0");
	var foreignFrozen = other.Object.freeze(other.Array.of(0, 1));
	setSloppy(foreignFrozen, 0);
	assert.sameValue(thrown(() => setStrict(foreignFrozen, 0)), TypeError);
	assert.sameValue(foreignFrozen.length, 2);

	var calls = [];
	var withSetter = {
		set length(v) {
			calls.push(v);
		},
	};
	setStrict(withSetter, 1);
	assert.sameValue(calls.join(), "1");
	var plain = { length: 5, 0: 0 };
	setStrict(plain, 0);
	assert.sameValue(plain.length, 0);
	assert.sameValue(plain[0], 0);
	var args = (function () { return arguments; })(0, 1, 2);
	setStrict(args, 1);
	assert.sameValue(args.length, 1);
	assert.sameValue(args[2], 2);

	var typed = new Uint8Array(4);
	setSloppy(typed, 1);
	assert.sameValue(typed.length, 4);
	assert.sameValue(thrown(() => setStrict(typed, 1)), TypeError);
	setSloppy("abc", 1);
	assert.sameValue(thrown(() => setStrict("abc", 1)), TypeError);
	function fn(x, y) {}
	setSloppy(fn, 0);
	assert.sameValue(fn.length, 2);
	assert.sameValue(thrown(() => setStrict(fn, 0)), TypeError);
}
