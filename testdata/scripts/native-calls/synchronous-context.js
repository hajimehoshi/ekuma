// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Cross-realm constructors and tail calls restore the surviving caller's realm"
includes: [realm-assertions.js]
---*/

const B = $262.createRealm();
const C = $262.createRealm();
const check = realmAssertions({ A: $262, B, C });
const throwInC = C.evalScript("(function () { 'use strict'; return arguments.callee; })");
const tail = B.evalScript("(function (callee) { 'use strict'; return callee(); })");
const makeArray = C.evalScript("(() => [])");
const Bridge = B.evalScript(`(function (tail, callee) {
    try {
        this.result = tail(callee);
    } finally {
        this.after = [];
    }
})`);

const result = new Bridge(tail, makeArray);
check.prototype(result.result, "C", "Array", "tail callee");
check.prototype(result.after, "B", "Array", "constructor after the tail call");
check.prototype([], "A", "Array", "caller after construction");

const receiver = {};
check.throws(() => Bridge.call(receiver, tail, throwInC), "C", "TypeError", "tail callee throw");
check.prototype(receiver.after, "B", "Array", "finally in B after the throw");
check.prototype([], "A", "Array", "caller after the throw");
