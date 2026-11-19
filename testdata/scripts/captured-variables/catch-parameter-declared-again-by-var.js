// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "catch parameter declared again by var"
---*/

function f1() { try { throw 1; } catch (e) { var g = () => e; var e = 2; return g(); } }
function f2() { try { throw 1; } catch (e) { var g = function () { return e; }; var [e] = [2]; return g(); } }
function f3() { try { throw 1; } catch (e) { var g = () => e; for (var e in { x: 0 }); return g(); } }
assert.sameValue([f1(), f2(), f3()].join(), "2,2,x");
