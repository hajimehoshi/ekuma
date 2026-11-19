// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "class bodies in sloppy code read and write bindings as strict code"
flags: [noStrict]
---*/

function errorName(f) {
  try {
    f();
  } catch (e) {
    return e.constructor.name;
  }
  return "no error";
}

// The has trap reports the binding name only to its first query, which resolves the binding,
// so the binding is gone when it is read.
function vanishing(target, name) {
  var queried = false;
  return new Proxy(target, {
    has: function(t, k) {
      if (k !== name) {
        return Reflect.has(t, k);
      }
      if (queried) {
        return false;
      }
      queried = true;
      return true;
    },
  });
}

var frozen = Object.freeze({x: 0});
assert.sameValue(errorName(function() {
  with (frozen) {
    (class { [x = 1]() {} });
  }
}), "TypeError", "assignment to a read-only property of a with scope");

var scope = vanishing({}, "y");
assert.sameValue(errorName(function() {
  with (scope) {
    (class { [y]() {} });
  }
}), "ReferenceError", "read of a with-scope binding that is gone");

Object.setPrototypeOf(globalThis, vanishing(Object.getPrototypeOf(globalThis), "z"));
assert.sameValue(errorName(function() {
  (class { [z]() {} });
}), "ReferenceError", "read of a global binding that is gone");

Object.setPrototypeOf(globalThis, vanishing(Object.getPrototypeOf(globalThis), "w"));
assert.sameValue(errorName(function() {
  (class { [typeof w]() {} });
}), "ReferenceError", "typeof of a global binding that is gone");
