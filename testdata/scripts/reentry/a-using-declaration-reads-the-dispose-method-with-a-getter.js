// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a function continues with its variables after a using declaration read the dispose method by calling a getter"
---*/

var log = [];
function f() {
  var v = "local";
  {
    using resource = {
      get [Symbol.dispose]() {
        log.push("get");
        return function () {
          log.push("dispose");
        };
      },
    };
    log.push(v);
  }
  return v;
}
assert.sameValue(f(), "local");
assert.sameValue(log.join(), "get,local,dispose");
