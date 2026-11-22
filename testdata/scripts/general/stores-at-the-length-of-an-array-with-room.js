// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "stores at the length of an array that has room for more elements"
---*/

var strict = (function () { return this === undefined; })();
var refused = strict ? "TypeError" : "done";

// five returns an array of five elements with room for more.
function five() {
  var a = [];
  for (var i = 0; i < 5; i++) {
    a[i] = i;
  }
  return a;
}

function store(a, v) {
  try {
    a[5] = v;
  } catch (e) {
    return e.constructor.name;
  }
  return "done";
}

function own(a) {
  return [a.length, a.hasOwnProperty(5)].join();
}

var log = [];
function withSetter(o, name) {
  return Object.defineProperty(o, 5, {
    set(v) { log.push(name + " " + v); },
    configurable: true,
  });
}

var a = five();
assert.sameValue(store(a, 0), "done");
assert.sameValue(String(a), "0,1,2,3,4,0");

a = five();
Object.preventExtensions(a);
assert.sameValue(store(a, 1), refused);
assert.sameValue(own(a), "5,false");

a = five();
Object.defineProperty(a, "length", {writable: false});
assert.sameValue(store(a, 2), refused);
assert.sameValue(own(a), "5,false");

a = five();
a.length = 8;
assert.sameValue(store(a, 3), "done");
assert.sameValue(own(a), "8,true");

a = five();
Object.setPrototypeOf(a, withSetter(Object.create(Array.prototype), "own prototype"));
assert.sameValue(store(a, 4), "done");
assert.sameValue(own(a), "5,false");

// Array.prototype inherits the setter from an object between it and Object.prototype.
a = five();
Object.setPrototypeOf(Array.prototype, Object.create(withSetter({}, "between")));
assert.sameValue(store(a, 5), "done");
Object.setPrototypeOf(Array.prototype, Object.prototype);
assert.sameValue(own(a), "5,false");

// An accessor leaves the indices of Array.prototype sparse for good, so this comes last.
a = five();
withSetter(Array.prototype, "Array.prototype");
assert.sameValue(store(a, 6), "done");
assert.sameValue(own(a), "5,false");

assert.sameValue(log.join(), "own prototype 4,between 5,Array.prototype 6");
