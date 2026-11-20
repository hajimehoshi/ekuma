// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser_test

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/srcpos"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type mode uint8

const (
	script mode = iota
	strict
	module
)

func parse(src wtf8.String, m mode) (*ast.Program, error) {
	switch m {
	case strict:
		return parser.ParseScript(src, parser.Options{
			Strict: true,
		})
	case module:
		return parser.ParseModule(src, parser.Options{})
	}
	return parser.ParseScript(src, parser.Options{})
}

func mustParse(t *testing.T, src wtf8.String, m mode) *ast.Program {
	t.Helper()
	prog, err := parse(src, m)
	if err != nil {
		t.Fatalf("parse(%q) failed: %v", src, err)
	}
	return prog
}

func TestValid(t *testing.T) {
	testCases := []struct {
		src  wtf8.String
		mode mode
	}{
		// Lexical grammar.
		{
			src: "#!/usr/bin/env js\n1",
		},
		{
			src: "0x1F + 0o17 + 0b11 + 1_000.5e-3 + .5 + 5. + 017 + 089.5 + 10n + 0xFFn",
		},
		{
			src: "'\\x41\\u0041\\u{1F600}\\0\\\n\\8' + \"\u2028\"",
		},
		{
			src: "var \\u0061b = 1, a\\u{62}, $_ = ab, été, 中",
		},
		{
			src: "x = a / b / c; y = /re[/]\\//gi.test(s); z = a++ / 2",
		},
		{
			src: "<!-- html comment\nx\n--> close comment\n/* a */ --> also",
		},
		{
			src:  "x = a<!--b\n",
			mode: script,
		},
		{
			src: "`a${b}c${`d${e}`}f` + tag`\\unicode and \\xerxes ${1}`",
		},
		{
			src: "a?.b?.[c]?.(d).e + (a?.b).c + a?.5:1",
		},
		{
			src: "l\\u0065t\na",
		},

		// Expressions.
		{
			src: "a ?? b ?? c; (a || b) ?? c; a ?? (b && c); a ||= b; a &&= b; a ??= b",
		},
		{
			src: "2 ** 3 ** 2; (-1) ** 2; x **= 2; -(1 ** 2)",
		},
		{
			src: "new new X()(); new X; new X.y(); new (f())(); new a.b`t`",
		},
		{
			src: "x = function* () { yield; yield 1; yield* g(); x = yield }",
		},
		{
			src: "async function f() { await x; for await (const y of z); for await (async of z); }",
		},
		{
			src: "x => x; (x) => x; (x, y) => ({}); async x => x; async (x) => x; () => {}; (a = 1, [b], {c}, ...d) => 0",
		},
		{
			src: "async\n(x); async (x); var async; async = 1; async\nfunction f() {}",
		},
		{
			src: "({a, b: c, [d]: e, 'f': g, 1: h, get i() {}, set i(v) {}, j() {}, *k() {}, async l() {}, async *m() {}, ...n, get, set, async, static: 1})",
		},
		{
			src: "({__proto__: a, __proto__: b} = x); ({__proto__: 1, ['__proto__']: 2, __proto__() {}, __proto__})",
		},
		{
			src: "[a, b = 1, [c], {d}, ...e] = x; ({a, b = 1, c: [d], ...e} = x); [(a), (b.c), d.e] = x; [...[a]] = x",
		},
		{
			src: "({a = 1}) => a; [{a = 1}] = x; ({a = 1} = x); for ({a = 1} of x); ({a: {b = 1}} = x)",
		},
		{
			src: "(a, b); (a = 1); ((a)) = 1; (a.b) = 1; (a) += 1; (a)++",
		},
		{
			src: "let = 1; let\nx; var yield, await, static, async, of, get, set; yield: 1;",
		},
		{
			src: "x = class A extends B { static #p = 1; #q; static { this.#p; } static m() {} get #g() {} set #g(v) {} #m() { return #q in this; } 'constructor'() { super(); } static async *[k]() {} static; get; set = 1; async\n x() {} }",
		},
		{
			src: "class A { constructor() { new.target; } a = this; b = () => this; static c = super.x; [a + b] = 1 }",
		},
		{
			src: "class A extends B { constructor() { (() => super())(); } m() { super.m(); super[x]; } }",
		},
		{
			src: "class A { #a; m() { class B { n(o) { return o.#a; } } } }",
		},
		{
			src: "o = { m() { return super.x; }, get g() { return super.y; } }",
		},
		{
			src: "function f() { new.target; return () => new.target; }",
		},
		{
			src:  "import('m'); import('m', {with: {}},); x = import.meta",
			mode: module,
		},
		{
			src: "a = b ? c => d : e => f; a = b ? (c) : d => e",
		},
		{
			src: "x = a ? b in c : d; for (var i = (a in b); ; ) break; for (var i = [a in b]; ; ) break",
		},
		{
			src: "typeof void delete a.b, !~+-a, ++a, --a, a++, a--",
		},
		{
			src: "eval('x'); (eval)('x'); (0, eval)('x'); eval?.('x')",
		},
		{
			src: "a\n++b; a\n/b/g; x = y\n(z); a = b\n`c`",
		},
		{
			src: "f(...a, b, ...c); [...a, , b]; new F(...a)",
		},

		// Statements.
		{
			src: "if (a) b; else if (c) d; else { e }",
		},
		{
			src: "for (;;) break; for (var i = 0, j; i < j; i++) continue; for (x in y); for (var x in y); for (let x of y); for (const [a, b] of y); for (x.y of z); for ([a, b] of z); for (let in y); for (let;;) break; for (let = 1;;) break",
		},
		{
			src: "for (var a = 1 in b);",
		},
		{
			src: "a: b: for (;;) { continue a; break b; } c: { break c; } d: function f() {}",
		},
		{
			src: "do x; while (y) z; do ; while (y)\nz",
		},
		{
			src: "switch (a) { case 1: let x; break; default: function f() {} case 2: }",
		},
		{
			src: "try {} catch {} try {} catch (e) { var e; } finally {} try {} catch ({a, b: [c]}) {}",
		},
		{
			src: "try {} catch (e) { for (var e of []); }",
		},
		{
			src: "with (a) b; if (a) function f() {} else function g() {}",
		},
		{
			src: "{ function f() {} function f() {} } var f; { let g; } var g;",
		},
		{
			src: "function f(a, a) {} function g(a) { var a; function a() {} } function h() { var x; var x; { var x; } }",
		},
		{
			src: "function f(a = 1, {b}, [c] = [], ...d) { 'use\\x20strict'; with (a); }",
		},
		{
			src: "function f() { 'use strict'; function g() {} }\nfunction yield() {}\nfunction* g() { (function yield() {}) }",
		},
		{
			src:  "async function f() { var g = function await() {}; }",
			mode: script,
		},
		{
			src: "var x = function* yield_() {}; var await; async function await() {}",
		},
		{
			src: "debugger; ; throw a\nvar b\n",
		},
		{
			src: "function f() { return\n1 }",
		},
		{
			src: "label: { label2: for (;;) { function g() { label: label2: ; } } }",
		},

		// Strict mode.
		{
			src:  "var o = {eval: 1, arguments: 2}; o.eval = 1; (function () { arguments; })",
			mode: strict,
		},
		{
			src:  "class C { static { var await_; } }; x = { yield: 1, let: 2, static: 3 }",
			mode: strict,
		},
		{
			src: "class C { static { x = () => await; } }",
		},

		// Modules.
		{
			src:  "import a from 'a'; import * as b from 'b'; import {c, d as e, default as f, 'g h' as i} from 'c'; import j, {k} from 'd'; import l, * as m from 'e'; import 'f'; import {} from 'g'",
			mode: module,
		},
		{
			src:  "import a from 'a' with {type: 'json', 'x': 'y'}; export {b} from 'b' with {}; export * from 'c' with {type: 'json'}",
			mode: module,
		},
		{
			src:  "export var a, [b, {c}] = x; export let d; export const e = 1; export function f() {} export class G {} export async function h() {} export function* i() {}",
			mode: module,
		},
		{
			src:  "var a, b; export {a, b as c, a as 'd e'}; export * from 'm'; export * as n from 'm'; export * as 'o p' from 'm'; export {if, 'x' as y, default} from 'm'",
			mode: module,
		},
		{
			src:  "export default function () {}",
			mode: module,
		},
		{
			src:  "export default class {}",
			mode: module,
		},
		{
			src:  "export default async function* f() {}; f",
			mode: module,
		},
		{
			src:  "export default a = 1\nvar a",
			mode: module,
		},
		{
			src:  "await 1; for await (x of y); var x, y; export {x}; function f() { return; }",
			mode: module,
		},
		{
			src:  "export {a}; import {a} from 'm'",
			mode: module,
		},
		{
			src:  "-->",
			mode: script,
		},

		// Decorators.
		{
			src:  "@a export class A {} export @b class B {} export default @c class {}",
			mode: module,
		},
		{
			src: "class C { @a static constructor() {} @b accessor\nx; static accessor [k] = 1 }",
		},
	}
	for _, tc := range testCases {
		if _, err := parse(tc.src, tc.mode); err != nil {
			t.Errorf("parse(%q, %d) failed: %v", tc.src, tc.mode, err)
		}
	}
}

func TestInvalid(t *testing.T) {
	testCases := []struct {
		src  wtf8.String
		mode mode
	}{
		// Lexical grammar.
		{
			src: "1__0",
		},
		{
			src: "1_",
		},
		{
			src: "0_1",
		},
		{
			src: "0x",
		},
		{
			src: "3in x",
		},
		{
			src: "08n",
		},
		{
			src: "1.5n",
		},
		{
			src: "'abc",
		},
		{
			src: "'a\nb'",
		},
		{
			src: "'\\u{110000}'",
		},
		{
			src: "'\\xZ'",
		},
		{
			src: "`a${b",
		},
		{
			src: "`\\07`",
		},
		{
			src: "`\\u`",
		},
		{
			src: "/a",
		},
		{
			src: "/a/\\u0067",
		},
		{
			src: "/* a",
		},
		{
			src: "var \\u0030a",
		},
		{
			src: "var a\\u0020b",
		},
		{
			src: "v\\u0061r x",
		},
		{
			src: "x = \\u0074his",
		},
		{
			src: "#x",
		},
		{
			src: "@",
		},
		{
			src: "x \xff",
		},
		{
			src:  "<!-- x",
			mode: module,
		},
		{
			src: "x; --> y",
		},
		{
			src: "\n#!x",
		},
		{
			src:  "017",
			mode: strict,
		},
		{
			src:  "089",
			mode: strict,
		},
		{
			src:  "'\\07'",
			mode: strict,
		},
		{
			src:  "'\\8'",
			mode: strict,
		},
		{
			src: "function f() { '\\07'; 'use strict'; }",
		},
		{
			src: "function f() { 'use strict'; 017 }",
		},

		// Expressions.
		{
			src: "a ?? b || c",
		},
		{
			src: "a || b ?? c",
		},
		{
			src: "a && b ?? c",
		},
		{
			src: "a ?? b && c",
		},
		{
			src: "-1 ** 2",
		},
		{
			src: "async function f() { await 1 ** 2 }",
		},
		{
			src: "a?.b`c`",
		},
		{
			src: "a?.b = 1",
		},
		{
			src: "a?.b++",
		},
		{
			src: "new a?.b()",
		},
		{
			src: "new.target",
		},
		{
			src: "new.foo",
		},
		{
			src: "function f() { n\\u0065w.target }",
		},
		{
			src: "function f() { new.t\\u0061rget }",
		},
		{
			src: "super.x",
		},
		{
			src: "function f() { super.x }",
		},
		{
			src: "class A { constructor() { super(); } }",
		},
		{
			src: "class A extends B { m() { super(); } }",
		},
		{
			src: "class A extends B { constructor() { function f() { super(); } } }",
		},
		{
			src: "({ m() { super(); } })",
		},
		{
			src: "({ m: function () { super.x; } })",
		},
		{
			src: "import.meta",
		},
		{
			src:  "import.meat",
			mode: module,
		},
		{
			src: "new import('m')",
		},
		{
			src: "import()",
		},
		{
			src: "import('a', 'b', 'c')",
		},
		{
			src: "1 = 2",
		},
		{
			src: "a + b = 2",
		},
		{
			src: "(a, b) = 2",
		},
		{
			src:  "f() = 1",
			mode: strict,
		},
		{
			src: "[f() = 1] = x",
		},
		{
			src: "this = 1",
		},
		{
			src: "({a}) = 1",
		},
		{
			src: "([a]) = 1",
		},
		{
			src: "[(a = 1)] = x",
		},
		{
			src: "[({a})] = x",
		},
		{
			src: "({a: ({b})} = x)",
		},
		{
			src: "[...a, b] = x",
		},
		{
			src: "[...a,] = x",
		},
		{
			src: "[...a = 1] = x",
		},
		{
			src: "({...a, b} = x)",
		},
		{
			src: "({...{a}} = x)",
		},
		{
			src: "({a() {}} = x)",
		},
		{
			src: "({get a() {}} = x)",
		},
		{
			src: "({a: 1} = x)",
		},
		{
			src: "[a] += x",
		},
		{
			src: "({a = 1})",
		},
		{
			src: "({a = 1}).b",
		},
		{
			src: "({a = 1}) = 1",
		},
		{
			src: "f({a = 1})",
		},
		{
			src: "[{a = 1}]",
		},
		{
			src: "({a = 1} + 1)",
		},
		{
			src: "({__proto__: 1, __proto__: 2})",
		},
		{
			src: "({__proto__: 1, '__proto__': 2})",
		},
		{
			src: "({a b})",
		},
		{
			src: "({if})",
		},
		{
			src: "({get a(b) {}})",
		},
		{
			src: "({set a() {}})",
		},
		{
			src: "({set a(...b) {}})",
		},
		{
			src: "({g\\u0065t a() {}})",
		},
		{
			src: "({async\nf() {}})",
		},
		{
			src: "()",
		},
		{
			src: "(a,)",
		},
		{
			src: "(...a)",
		},
		{
			src: "(a, ...b)",
		},
		{
			src: "() \n=> 0",
		},
		{
			src: "a \n=> 0",
		},
		{
			src: "async a \n=> 0",
		},
		{
			src: "function* g() { function yield() {} }",
		},
		{
			src: "async function f() { function await() {} }",
		},
		{
			src:  "export default a = 1, 2",
			mode: module,
		},
		{
			src: "\\u0061sync a => 0",
		},
		{
			src: "(a, a) => 0",
		},
		{
			src: "((a)) => 0",
		},
		{
			src: "(a, (b)) => 0",
		},
		{
			src: "([(a)]) => 0",
		},
		{
			src: "((a = 1)) => 0",
		},
		{
			src: "(a.b) => 0",
		},
		{
			src: "([a.b]) => 0",
		},
		{
			src: "(a + b) => 0",
		},
		{
			src: "(...a, b) => 0",
		},
		{
			src: "(...a,) => 0",
		},
		{
			src: "(...a = 1) => 0",
		},
		{
			src: "async (...a,) => 0",
		},
		{
			src: "async (a, a) => 0",
		},
		{
			src: "async (a = await 1) => 0",
		},
		{
			src: "async (await) => 0",
		},
		{
			src: "async await => 0",
		},
		{
			src: "async (x = (await) => 0) => 0",
		},
		{
			src: "function* g() { (a = yield) => 0 }",
		},
		{
			src: "function* g() { yield => 0 }",
		},
		{
			src: "async function f() { (a = await 1) => 0 }",
		},
		{
			src: "async function f() { (await) => 0 }",
		},
		{
			src: "async function f() { (...await) => 0 }",
		},
		{
			src: "async function f() { (...{await}) => 0 }",
		},
		{
			src: "function* g() { (...yield) => 0 }",
		},
		{
			src: "a + b => 0",
		},
		{
			src: "x = a => {} + 1",
		},
		{
			src: "a => {} ()",
		},
		{
			src: "a => a ? b : c => {} = 1",
		},
		{
			src: "(eval) => { 'use strict' }",
		},
		{
			src: "(a = 1) => { 'use strict' }",
		},
		{
			src: "(yield) => { 'use strict' }",
		},
		{
			src:  "eval => 0",
			mode: strict,
		},
		{
			src:  "(a, eval) => 0",
			mode: strict,
		},
		{
			src: "x = {a: 1",
		},
		{
			src: "typeof x => 0",
		},
		{
			src: "x = #a in b",
		},
		{
			src: "class A { #a; m() { 1 + #a in b } }",
		},
		{
			src: "class A { #a; m() { #a } }",
		},
		{
			src: "class A { #a; m() { #a in #a in b } }",
		},
		{
			src: "yield x",
		},
		{
			src: "function* g() { yield\n* 2 }",
		},
		{
			src: "function* g(a = yield) {}",
		},
		{
			src: "function* g() { function* h(a = yield) {} }",
		},
		{
			src: "function* g() { var yield }",
		},
		{
			src: "function* g() { yi\\u0065ld 1 }",
		},
		{
			src: "x = function* yield() {}",
		},
		{
			src: "async function f(a = await 1) {}",
		},
		{
			src: "async function f() { var await }",
		},
		{
			src: "async function f() { function g(a = await 1) {} }",
		},
		{
			src: "x = async function await() {}",
		},
		{
			src: "async function f() { aw\\u0061it 1 }",
		},
		{
			src: "await 1",
		},
		{
			src:  "function f() { await 1 }",
			mode: module,
		},
		{
			src:  "var await",
			mode: module,
		},
		{
			src:  "x = () => await 1",
			mode: module,
		},
		{
			src: "for await (x of y);",
		},
		{
			src: "async function f() { for await (x in y); }",
		},
		{
			src: "async function f() { for await (;;); }",
		},
		{
			src: "`${a`",
		},
		{
			src: "a ? b",
		},
		{
			src:  "delete x",
			mode: strict,
		},
		{
			src:  "delete (x)",
			mode: strict,
		},
		{
			src:  "delete ((x))",
			mode: strict,
		},
		{
			src: "class A { #a; m() { delete this.#a } }",
		},
		{
			src: "class A { #a; m() { delete (this.#a) } }",
		},
		{
			src: "class A { #a; m() { delete this?.#a } }",
		},
		{
			src: "++a++",
		},
		{
			src: "++1",
		},
		{
			src: "a\n++\nb++c",
		},
		{
			src: "for (a++ of b);",
		},

		// Strict mode.
		{
			src:  "eval = 1",
			mode: strict,
		},
		{
			src:  "arguments++",
			mode: strict,
		},
		{
			src:  "--eval",
			mode: strict,
		},
		{
			src:  "[eval] = x",
			mode: strict,
		},
		{
			src:  "({arguments} = x)",
			mode: strict,
		},
		{
			src:  "({a: eval = 1} = x)",
			mode: strict,
		},
		{
			src:  "({eval = 1} = x)",
			mode: strict,
		},
		{
			src:  "var eval",
			mode: strict,
		},
		{
			src:  "function arguments() {}",
			mode: strict,
		},
		{
			src:  "function f(eval) {}",
			mode: strict,
		},
		{
			src:  "try {} catch (eval) {}",
			mode: strict,
		},
		{
			src:  "let",
			mode: strict,
		},
		{
			src:  "var yield",
			mode: strict,
		},
		{
			src:  "var static",
			mode: strict,
		},
		{
			src:  "implements: 1",
			mode: strict,
		},
		{
			src:  "x = package",
			mode: strict,
		},
		{
			src:  "with (a) b",
			mode: strict,
		},
		{
			src:  "function f(a, a) {}",
			mode: strict,
		},
		{
			src:  "if (a) function f() {}",
			mode: strict,
		},
		{
			src:  "a: function f() {}",
			mode: strict,
		},
		{
			src:  "{ function f() {} function f() {} }",
			mode: strict,
		},
		{
			src:  "for (var a = 1 in b);",
			mode: strict,
		},
		{
			src: "function f(a, a) { 'use strict' }",
		},
		{
			src: "function eval() { 'use strict' }",
		},
		{
			src: "function f(interface) { 'use strict' }",
		},
		{
			src: "function f(a = 1) { 'use strict' }",
		},
		{
			src: "function f({a}) { 'use strict' }",
		},
		{
			src: "function f(...a) { 'use strict' }",
		},
		{
			src: "x = function static() { 'use strict' }",
		},
		{
			src: "'use strict'; var public",
		},
		{
			src: "'use strict'\nwith (a) b",
		},
		{
			src: "class yield {}",
		},
		{
			src: "class A { m(a, a) {} }",
		},
		{
			src: "class A extends (function () { with (a); }) {}",
		},
		{
			src: "var enum",
		},
		{
			src: "var if",
		},
		{
			src: "var x = enum",
		},

		// Declarations and scopes.
		{
			src: "let a; let a",
		},
		{
			src: "let a; var a",
		},
		{
			src: "var a; let a",
		},
		{
			src: "const a = 1; function a() {}",
		},
		{
			src: "class a {} var a",
		},
		{
			src: "let a; { var a }",
		},
		{
			src: "{ var a } let a",
		},
		{
			src: "{ let a; { var a } }",
		},
		{
			src: "{ let a; function a() {} }",
		},
		{
			src: "{ function a() {} var a }",
		},
		{
			src: "{ function a() {} function* a() {} }",
		},
		{
			src: "{ async function a() {} async function a() {} }",
		},
		{
			src: "function f(a) { let a }",
		},
		{
			src: "function f() { let a; var a }",
		},
		{
			src: "(a) => { let a }",
		},
		{
			src: "function f(a, a = 1) {}",
		},
		{
			src: "function f(a, [a]) {}",
		},
		{
			src: "function f(a, ...a) {}",
		},
		{
			src: "({ m(a, a) {} })",
		},
		{
			src: "let [a, a] = x",
		},
		{
			src: "let a, a",
		},
		{
			src: "let let = 1",
		},
		{
			src: "const let = 1",
		},
		{
			src: "for (let let of x);",
		},
		{
			src: "const a",
		},
		{
			src: "let [a]",
		},
		{
			src: "var {a}",
		},
		{
			src: "for (const a;;);",
		},
		{
			src: "for (let [a];;);",
		},
		{
			src: "for (let a of b) { var a }",
		},
		{
			src: "for (let a;;) { var a }",
		},
		{
			src: "for (let a, b of c);",
		},
		{
			src: "for (let a = 1 of c);",
		},
		{
			src: "for (var a = 1 of c);",
		},
		{
			src: "for (var [a] = 1 in c);",
		},
		{
			src: "for (let a = 1 in c);",
		},
		{
			src: "for (let of x);",
		},
		{
			src: "for (let.x of y);",
		},
		{
			src: "for (async of y);",
		},
		{
			src: "for (a, b of c);",
		},
		{
			src: "for (a = 1 of c);",
		},
		{
			src: "for ((a = 1) of c);",
		},
		{
			src: "for (({a}) of c);",
		},
		{
			src: "for (let [a, a] of c);",
		},
		{
			src: "for (a in b, c of d);",
		},
		{
			src: "for (a of b, c);",
		},
		{
			src: "for (var a, b in c);",
		},
		{
			src: "try {} catch (a) { let a }",
		},
		{
			src: "try {} catch ([a]) { var a }",
		},
		{
			src: "try {} catch (a) { function a() {} }",
		},
		{
			src: "try {} catch ([a, a]) {}",
		},
		{
			src: "try {}",
		},
		{
			src: "switch (a) { case 1: let b; case 2: let b }",
		},
		{
			src: "switch (a) { default: default: }",
		},
		{
			src: "if (a) let b",
		},
		{
			src: "if (a) const b = 1",
		},
		{
			src: "if (a) class B {}",
		},
		{
			src: "while (a) function f() {}",
		},
		{
			src: "if (a) function* f() {}",
		},
		{
			src: "if (a) async function f() {}",
		},
		{
			src: "if (a) let\n[b] = c",
		},
		{
			src: "a: let\n[b] = c",
		},
		{
			src: "a: function* f() {}",
		},
		{
			src: "a: async function f() {}",
		},
		{
			src: "if (a) b: function f() {}",
		},
		{
			src: "while (a) b: function f() {}",
		},
		{
			src: "a: a: ;",
		},
		{
			src: "a: { a: ; }",
		},
		{
			src: "break",
		},
		{
			src: "continue",
		},
		{
			src: "a: { continue a; }",
		},
		{
			src: "a: { break b; }",
		},
		{
			src: "for (;;) { function f() { break; } }",
		},
		{
			src: "a: for (;;) { function f() { continue a; } }",
		},
		{
			src: "a: for (;;) { x = () => { break a; } }",
		},
		{
			src: "switch (a) { case 1: continue; }",
		},
		{
			src: "return",
		},
		{
			src: "class A { static { return; } }",
		},
		{
			src: "class A { static { await 1; } }",
		},
		{
			src: "class A { static { var await; } }",
		},
		{
			src: "class A { static { x = (y = await) => 0; } }",
		},
		{
			src: "class A { static { arguments; } }",
		},
		{
			src: "class A { static { arguments: arguments; } }",
		},
		{
			src: "class A { get *a() {} }",
		},
		{
			src: "async function f() { class A { static { await 1; } } }",
		},
		{
			src: "throw\na",
		},
		{
			src: "function f() {",
		},
		{
			src: "}",
		},
		{
			src: "a b",
		},
		{
			src: "var a = 1 var b",
		},
		{
			src: "do x while (y)",
		},
		{
			src: "function () {}",
		},
		{
			src: "class {}",
		},
		{
			src: "function f(a,,) {}",
		},
		{
			src: "function f(...a,) {}",
		},
		{
			src: "function f(...a = 1) {}",
		},
		{
			src: "async\nfunction f() {}\nasync function",
		},

		// Classes.
		{
			src: "class A { constructor() {} constructor() {} }",
		},
		{
			src: "class A { get constructor() {} }",
		},
		{
			src: "class A { *constructor() {} }",
		},
		{
			src: "class A { async constructor() {} }",
		},
		{
			src: "class A { constructor = 1 }",
		},
		{
			src: "class A { 'constructor' }",
		},
		{
			src: "class A { static prototype() {} }",
		},
		{
			src: "class A { static prototype }",
		},
		{
			src: "class A { #constructor }",
		},
		{
			src: "class A { #a; #a }",
		},
		{
			src: "class A { #a() {} get #a() {} }",
		},
		{
			src: "class A { get #a() {} get #a() {} }",
		},
		{
			src: "class A { static get #a() {} set #a(v) {} }",
		},
		{
			src: "class A { m() { this.#a } }",
		},
		{
			src: "this.#a",
		},
		{
			src: "class A { #a } class B { m() { this.#a } }",
		},
		{
			src: "class A extends B.#a { #a }",
		},
		{
			src: "class A { a = arguments }",
		},
		{
			src: "class A { a = () => arguments }",
		},
		{
			src: "class A { a = super() }",
		},
		{
			src: "class A { [arguments] = 1; static { arguments } }",
		},
		{
			src: "class A { a b }",
		},
		{
			src: "class A { st\\u0061tic m() {} }",
		},
		{
			src: "class A { super.x }",
		},
		{
			src: "class A { m() { super.#a } }",
		},
		{
			src: "class A { x = yield }",
		},
		{
			src: "class A extends B, C {}",
		},

		// Modules.
		{
			src: "import a from 'a'",
		},
		{
			src: "export var a",
		},
		{
			src:  "{ import a from 'a' }",
			mode: module,
		},
		{
			src:  "function f() { export var a }",
			mode: module,
		},
		{
			src:  "if (a) import 'b'",
			mode: module,
		},
		{
			src:  "import {a, a} from 'm'",
			mode: module,
		},
		{
			src:  "import a, a from 'm'",
			mode: module,
		},
		{
			src:  "import a from 'm'; let a",
			mode: module,
		},
		{
			src:  "import {if} from 'm'",
			mode: module,
		},
		{
			src:  "import {'a'} from 'm'",
			mode: module,
		},
		{
			src:  "import {a as if} from 'm'",
			mode: module,
		},
		{
			src:  "import {eval} from 'm'",
			mode: module,
		},
		{
			src:  "import * from 'm'",
			mode: module,
		},
		{
			src:  "import {a} fr\\u006fm 'm'",
			mode: module,
		},
		{
			src:  "import {'\\uD800' as a} from 'm'",
			mode: module,
		},
		{
			src:  "import a from 'm' with {a: 'b', a: 'c'}",
			mode: module,
		},
		{
			src:  "import a from 'm' with {a: 1}",
			mode: module,
		},
		{
			src:  "export {a}",
			mode: module,
		},
		{
			src:  "export {a as b}; var b",
			mode: module,
		},
		{
			src:  "var a; export {a, a}",
			mode: module,
		},
		{
			src:  "var a; export {a as b}; export var b",
			mode: module,
		},
		{
			src:  "var a; export {a as default}; export default 1",
			mode: module,
		},
		{
			src:  "export default 1; export default 2",
			mode: module,
		},
		{
			src:  "export {if}",
			mode: module,
		},
		{
			src:  "export {'a'}",
			mode: module,
		},
		{
			src:  "var a; export {a as '\\uDC00'}",
			mode: module,
		},
		{
			src:  "export * as a from 'm'; export var a",
			mode: module,
		},
		{
			src:  "export function f() {} export function f() {}",
			mode: module,
		},
		{
			src:  "function f() {} function f() {}",
			mode: module,
		},
		{
			src:  "export default function () {} export default class {}",
			mode: module,
		},
		{
			src:  "export a",
			mode: module,
		},
		{
			src:  "export async () => {}",
			mode: module,
		},
		{
			src:  "export let\nx = 1, x",
			mode: module,
		},
		{
			src:  "a: function f() {}",
			mode: module,
		},
		{
			src:  "x = 017",
			mode: module,
		},
		{
			src:  "await: 1",
			mode: module,
		},
		{
			src:  "new.target",
			mode: module,
		},

		// Decorators.
		{
			src: "class C { @a constructor() {} }",
		},
		{
			src: "class C { @a static {} }",
		},
		{
			src: "class C { @a; }",
		},
		{
			src: "class C { @a[0] m() {} }",
		},
		{
			src: "class C { @a() () {} }",
		},
		{
			src: "@a function f() {}",
		},
		{
			src: "if (x) @a class C {}",
		},
		{
			src:  "@a export const x = 1",
			mode: module,
		},
		{
			src:  "@a export @b class C {}",
			mode: module,
		},
		{
			src: "class C { accessor m() {} }",
		},
		{
			src: "class C { accessor #x; get #x() {} }",
		},
		{
			src: "class C { accessor constructor }",
		},
	}
	for _, tc := range testCases {
		prog, err := parse(tc.src, tc.mode)
		if err == nil {
			t.Errorf("parse(%q, %d) succeeded, want an error", tc.src, tc.mode)
			continue
		}
		var perr *parser.Error
		if !errors.As(err, &perr) {
			t.Errorf("parse(%q, %d): error type is %T, want *parser.Error", tc.src, tc.mode, err)
			continue
		}
		if prog != nil {
			t.Errorf("parse(%q, %d) returned a program together with an error", tc.src, tc.mode)
		}
		if perr.Offset < 0 || perr.Offset > len(tc.src) || perr.Line < 1 || perr.Column < 1 {
			t.Errorf("parse(%q, %d): invalid error position: offset %d, line %d, column %d", tc.src, tc.mode, perr.Offset, perr.Line, perr.Column)
		}
	}
}

func TestErrorPosition(t *testing.T) {
	_, err := parse("var a;\r\nvar é = #;", script)
	var perr *parser.Error
	if !errors.As(err, &perr) {
		t.Fatalf("error type is %T, want *parser.Error", err)
	}
	if got, want := perr.Offset, 17; got != want {
		t.Errorf("Offset: got %d, want %d", got, want)
	}
	if got, want := perr.Line, 2; got != want {
		t.Errorf("Line: got %d, want %d", got, want)
	}
	if got, want := perr.Column, 9; got != want {
		t.Errorf("Column: got %d, want %d", got, want)
	}
}

func TestDeepNesting(t *testing.T) {
	for _, src := range []string{
		strings.Repeat("(", 100000),
		strings.Repeat("[", 100000),
		strings.Repeat("{", 100000),
		strings.Repeat("!", 100000) + "a",
		strings.Repeat("a=", 100000) + "a",
		strings.Repeat("a?b:", 100000) + "a",
		strings.Repeat("new ", 100000) + "a",
		strings.Repeat("a=>", 100000) + "a",
		strings.Repeat("if(a)", 100000) + ";",
		strings.Repeat("class A extends ", 100000) + "B{}",
		strings.Repeat("var [", 1) + strings.Repeat("[", 100000),
		strings.Repeat("`${", 100000),
		strings.Repeat("a**", 100000) + "a",
		strings.Repeat("function f(a=", 100000),
	} {
		_, err := parse(wtf8.String(src), script)
		if _, ok := errors.AsType[*parser.Error](err); !ok {
			t.Errorf("parse(%.20q...): got error %v, want *parser.Error", src, err)
		}
	}

	// A long chain is not nested syntax, but its length is limited, also across chains in
	// the operands of a chain.
	if _, err := parse(wtf8.String(strings.Repeat("a+", parser.MaxLinks)+"a"), script); err != nil {
		t.Errorf("parse of a long binary chain failed: %v", err)
	}
	for _, src := range []string{
		strings.Repeat("a+", parser.MaxLinks+1) + "a",
		"a" + strings.Repeat(".a", parser.MaxLinks+1),
		"new a" + strings.Repeat(".a", parser.MaxLinks+1),
		"a+(a" + strings.Repeat("()", parser.MaxLinks) + ")",
	} {
		_, err := parse(wtf8.String(src), script)
		if _, ok := errors.AsType[*parser.Error](err); !ok {
			t.Errorf("parse(%.20q...): got error %v, want *parser.Error", src, err)
		}
	}
}

func exprOf(t *testing.T, src wtf8.String, m mode) ast.Expr {
	t.Helper()
	prog := mustParse(t, src, m)
	if len(prog.Body) == 0 {
		t.Fatalf("parse(%q): no statements", src)
	}
	s, ok := prog.Body[len(prog.Body)-1].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("parse(%q): last statement is %T, want *ast.ExprStmt", src, prog.Body[len(prog.Body)-1])
	}
	return s.Expr
}

func TestArrowCover(t *testing.T) {
	e := exprOf(t, "(a, {b, c: [d = 1]}, [e] = [], ...f) => a", script)
	fn, ok := e.(*ast.Function)
	if !ok || !fn.Arrow {
		t.Fatalf("got %T, want an arrow function", e)
	}
	if got, want := len(fn.Params), 4; got != want {
		t.Fatalf("len(Params): got %d, want %d", got, want)
	}
	if _, ok := fn.Params[0].(*ast.Identifier); !ok {
		t.Errorf("Params[0]: got %T, want *ast.Identifier", fn.Params[0])
	}
	if obj, ok := fn.Params[1].(*ast.ObjectPattern); !ok {
		t.Errorf("Params[1]: got %T, want *ast.ObjectPattern", fn.Params[1])
	} else if arr, ok := obj.Properties[1].Value.(*ast.ArrayPattern); !ok {
		t.Errorf("Params[1] c: got %T, want *ast.ArrayPattern", obj.Properties[1].Value)
	} else if _, ok := arr.Elements[0].(*ast.AssignPattern); !ok {
		t.Errorf("Params[1] c[0]: got %T, want *ast.AssignPattern", arr.Elements[0])
	}
	if _, ok := fn.Params[2].(*ast.AssignPattern); !ok {
		t.Errorf("Params[2]: got %T, want *ast.AssignPattern", fn.Params[2])
	}
	if _, ok := fn.Params[3].(*ast.RestElement); !ok {
		t.Errorf("Params[3]: got %T, want *ast.RestElement", fn.Params[3])
	}
	if got, want := fn.Length, 2; got != want {
		t.Errorf("Length: got %d, want %d", got, want)
	}
	if fn.SimpleParams {
		t.Errorf("SimpleParams: got true, want false")
	}
	if fn.ExprBody == nil {
		t.Errorf("ExprBody: got nil")
	}
	var names []string
	for _, d := range fn.Scope.Decls {
		if d.Kind != ast.DeclParam {
			t.Errorf("decl %s: got kind %d, want DeclParam", d.Name, d.Kind)
		}
		names = append(names, d.Name)
	}
	if got, want := strings.Join(names, ","), "a,b,d,e,f"; got != want {
		t.Errorf("parameter names: got %s, want %s", got, want)
	}

	// The same text without an arrow is a sequence expression in parentheses.
	if _, ok := exprOf(t, "(a, {b}, [e] = [])", script).(*ast.SequenceExpr); !ok {
		t.Errorf("parenthesized list: want *ast.SequenceExpr")
	}

	// async is a plain identifier unless an arrow follows on the same line.
	if c, ok := exprOf(t, "async(a, b)", script).(*ast.CallExpr); !ok || len(c.Args) != 2 {
		t.Errorf("async(a, b): got %T, want a call with two arguments", exprOf(t, "async(a, b)", script))
	}
	if fn, ok := exprOf(t, "async ({a}, b) => {}", script).(*ast.Function); !ok || !fn.Async || !fn.Arrow || len(fn.Params) != 2 {
		t.Errorf("async ({a}, b) => {}: want an async arrow function with two parameters")
	}
}

func TestAssignmentPattern(t *testing.T) {
	e := exprOf(t, "[a, , {b, c: d.e = 1, ...f}, ...g] = x", script)
	a, ok := e.(*ast.AssignExpr)
	if !ok {
		t.Fatalf("got %T, want *ast.AssignExpr", e)
	}
	arr, ok := a.Target.(*ast.ArrayPattern)
	if !ok {
		t.Fatalf("Target: got %T, want *ast.ArrayPattern", a.Target)
	}
	if got, want := len(arr.Elements), 4; got != want {
		t.Fatalf("len(Elements): got %d, want %d", got, want)
	}
	if arr.Elements[1] != nil {
		t.Errorf("Elements[1]: got %T, want a hole", arr.Elements[1])
	}
	if _, ok := arr.Elements[3].(*ast.RestElement); !ok {
		t.Errorf("Elements[3]: got %T, want *ast.RestElement", arr.Elements[3])
	}
	obj, ok := arr.Elements[2].(*ast.ObjectPattern)
	if !ok {
		t.Fatalf("Elements[2]: got %T, want *ast.ObjectPattern", arr.Elements[2])
	}
	if !obj.Properties[0].Shorthand {
		t.Errorf("property b: Shorthand is false")
	}
	if ap, ok := obj.Properties[1].Value.(*ast.AssignPattern); !ok {
		t.Errorf("property c: got %T, want *ast.AssignPattern", obj.Properties[1].Value)
	} else if _, ok := ap.Target.(*ast.MemberExpr); !ok {
		t.Errorf("property c target: got %T, want *ast.MemberExpr", ap.Target)
	}
	if id, ok := obj.Rest.(*ast.Identifier); !ok || id.Name != "f" {
		t.Errorf("Rest: got %T, want the identifier f", obj.Rest)
	}

	// Without an assignment the same text stays a literal.
	if _, ok := exprOf(t, "[a, , {b, c: d.e, ...f}, ...g]", script).(*ast.ArrayLit); !ok {
		t.Errorf("array literal: want *ast.ArrayLit")
	}

	// A parenthesized identifier target is marked.
	a = exprOf(t, "(a) = function () {}", script).(*ast.AssignExpr)
	if id, ok := a.Target.(*ast.Identifier); !ok || !id.Parenthesized {
		t.Errorf("(a) = ...: the target is not marked as parenthesized")
	}
}

func TestExpressionShape(t *testing.T) {
	// A parenthesis ends an optional chain.
	e := exprOf(t, "(a?.b.c).d", script)
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		t.Fatalf("(a?.b.c).d: got %T, want *ast.MemberExpr", e)
	}
	chain, ok := m.Object.(*ast.OptionalChain)
	if !ok {
		t.Fatalf("(a?.b.c).d object: got %T, want *ast.OptionalChain", m.Object)
	}
	inner := chain.Expr.(*ast.MemberExpr)
	if inner.Optional || !inner.Object.(*ast.MemberExpr).Optional {
		t.Errorf("a?.b.c: the Optional flags are wrong")
	}
	if _, ok := exprOf(t, "a?.b.c.d", script).(*ast.OptionalChain); !ok {
		t.Errorf("a?.b.c.d: want *ast.OptionalChain at the top")
	}
	if c, ok := exprOf(t, "a?.(b)", script).(*ast.OptionalChain).Expr.(*ast.CallExpr); !ok || !c.Optional {
		t.Errorf("a?.(b): want an optional call")
	}

	// A slash is a division or a regular expression depending on what precedes it.
	if b, ok := exprOf(t, "a / b / c", script).(*ast.BinaryExpr); !ok || b.Op != ast.OpDiv {
		t.Errorf("a / b / c: want a division")
	}
	if r, ok := exprOf(t, "a\n/b/g", script).(*ast.BinaryExpr); !ok || r.Op != ast.OpDiv {
		t.Errorf("a\\n/b/g: want a division")
	}
	if r, ok := exprOf(t, "x = /=[/]/u", script).(*ast.AssignExpr).Value.(*ast.RegExpLit); !ok || r.Pattern != "=[/]" || r.Flags != "u" {
		t.Errorf("/=[/]/u: want a regular expression literal")
	}
	if _, ok := exprOf(t, "() => {}\n/b/g", script).(*ast.RegExpLit); !ok {
		t.Errorf("a regular expression after an arrow function body: want *ast.RegExpLit")
	}

	// Precedence and associativity.
	b := exprOf(t, "a + b * c ** d ** e", script).(*ast.BinaryExpr)
	if b.Op != ast.OpAdd {
		t.Fatalf("top operator: got %v, want +", b.Op)
	}
	mul := b.Right.(*ast.BinaryExpr)
	exp := mul.Right.(*ast.BinaryExpr)
	if mul.Op != ast.OpMul || exp.Op != ast.OpExp || exp.Right.(*ast.BinaryExpr).Op != ast.OpExp {
		t.Errorf("a + b * c ** d ** e: wrong shape")
	}
	if l, ok := exprOf(t, "a - b - c", script).(*ast.BinaryExpr).Left.(*ast.BinaryExpr); !ok || l.Op != ast.OpSub {
		t.Errorf("a - b - c: want left associativity")
	}

	// let is an identifier in sloppy mode code.
	if id, ok := exprOf(t, "let", script).(*ast.Identifier); !ok || id.Name != "let" {
		t.Errorf("let: want an identifier")
	}
	prog := mustParse(t, "let\nlet_ = 1", script)
	if d, ok := prog.Body[0].(*ast.VarDecl); !ok || d.Kind != ast.DeclLet {
		t.Errorf("let followed by an identifier on the next line: got %T, want a let declaration", prog.Body[0])
	}

	// yield and await are identifiers outside generators and async functions.
	if _, ok := exprOf(t, "yield + await", script).(*ast.BinaryExpr); !ok {
		t.Errorf("yield + await: want a binary expression")
	}
	y := exprOf(t, "function* g() { yield\na }; 0; (function* () { yield /a/ })", script).(*ast.Function)
	if ye, ok := y.Body[0].(*ast.ExprStmt).Expr.(*ast.YieldExpr); !ok {
		t.Errorf("yield /a/: want *ast.YieldExpr")
	} else if _, ok := ye.Arg.(*ast.RegExpLit); !ok {
		t.Errorf("yield /a/: got argument %T, want *ast.RegExpLit", ye.Arg)
	}
}

func TestLiterals(t *testing.T) {
	testCases := []struct {
		src  wtf8.String
		want wtf8.String
	}{
		{
			src:  `'a\x41\u00e9\n'`,
			want: "aAé\n",
		},
		{
			src:  `'\uD83D\uDE00'`,
			want: "\U0001F600",
		},
		{
			src:  `'\uD83D\u{DE00}'`,
			want: "\U0001F600",
		},
		{
			src:  "'\\uD83D\\\n\\uDE00'",
			want: "\U0001F600",
		},
		{
			src:  `'\uD83D'`,
			want: "\xed\xa0\xbd",
		},
		{
			src:  `'\uDE00\uD83D'`,
			want: "\xed\xb8\x80\xed\xa0\xbd",
		},
		{
			src:  `'\uD83Dx\uDE00'`,
			want: "\xed\xa0\xbdx\xed\xb8\x80",
		},
		{
			src:  `'\101\0\8'`,
			want: "A\x008",
		},
		{
			src:  `'\u{1F600}'`,
			want: "\U0001F600",
		},
	}
	for _, tc := range testCases {
		lit, ok := exprOf(t, tc.src, script).(*ast.StringLit)
		if !ok {
			t.Errorf("%s: not a string literal", tc.src)
			continue
		}
		if lit.Value != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, lit.Value, tc.want)
		}
	}

	numbers := []struct {
		src  wtf8.String
		want float64
	}{
		{
			src:  "0x1F",
			want: 31,
		},
		{
			src:  "0o17",
			want: 15,
		},
		{
			src:  "0b101",
			want: 5,
		},
		{
			src:  "017",
			want: 15,
		},
		{
			src:  "089",
			want: 89,
		},
		{
			src:  "1_000.5e1",
			want: 10005,
		},
		{
			src:  ".5",
			want: 0.5,
		},
		{
			src:  "0xFFFFFFFFFFFFFFFFFF",
			want: 4722366482869645213696,
		},
		{
			src:  "9007199254740993",
			want: 9007199254740992,
		},
		{
			// Only the leading digits are converted, however many follow.
			src:  wtf8.String("0o" + strings.Repeat("7", 1<<23)),
			want: math.Inf(1),
		},
		{
			src:  wtf8.String("0o1" + strings.Repeat("0", 340)),
			want: math.Ldexp(1, 1020),
		},
	}
	for _, tc := range numbers {
		lit, ok := exprOf(t, tc.src, script).(*ast.NumberLit)
		if !ok {
			t.Errorf("%s: not a number literal", tc.src)
			continue
		}
		if lit.Value != tc.want {
			t.Errorf("%.20s: got %v, want %v", tc.src, lit.Value, tc.want)
		}
	}

	if lit, ok := exprOf(t, "0x1_Fn", script).(*ast.BigIntLit); !ok || lit.Value != "31" {
		t.Errorf("0x1_Fn: want the BigInt 31")
	}

	tt := exprOf(t, "tag`a\\u{41}\r\n${b}\\u{`", script).(*ast.TaggedTemplate).Quasi
	if got, want := len(tt.Quasis), 2; got != want {
		t.Fatalf("len(Quasis): got %d, want %d", got, want)
	}
	if q := tt.Quasis[0]; !q.HasCooked || q.Cooked != "aA\n" || q.Raw != "a\\u{41}\n" {
		t.Errorf("Quasis[0]: got cooked %q (%t), raw %q", q.Cooked, q.HasCooked, q.Raw)
	}
	if q := tt.Quasis[1]; q.HasCooked || q.Raw != "\\u{" {
		t.Errorf("Quasis[1]: got cooked %q (%t), raw %q", q.Cooked, q.HasCooked, q.Raw)
	}

	if id, ok := exprOf(t, "\\u0061\\u{62}c", script).(*ast.Identifier); !ok || id.Name != "abc" {
		t.Errorf("escaped identifier: want abc")
	}
}

func TestASI(t *testing.T) {
	testCases := []struct {
		src  wtf8.String
		want int
	}{
		{
			src:  "a\nb",
			want: 2,
		},
		{
			src:  "a\n(b)",
			want: 1,
		},
		{
			src:  "a\n[b]",
			want: 1,
		},
		{
			src:  "a\n++\nb",
			want: 2,
		},
		{
			src:  "a\n+b",
			want: 1,
		},
		{
			src:  "var a = 1\nvar b = 2",
			want: 2,
		},
		{
			src:  "x = function () {}\n;[1].map(f)",
			want: 2,
		},
		{
			src:  "{ a } b",
			want: 2,
		},
		{
			src:  "do a; while (b) c",
			want: 2,
		},
		{
			src:  "a = b\n/c/g.d",
			want: 1,
		},
		{
			src:  "if (a) b\nelse c",
			want: 1,
		},
		{
			src:  "let\na",
			want: 1,
		},
		{
			src:  "async\nfunction f() {}",
			want: 2,
		},
		{
			src:  "x => {}\n(y)",
			want: 2,
		},
		{
			src:  "a\n`b`",
			want: 1,
		},
	}
	for _, tc := range testCases {
		prog, err := parse(tc.src, script)
		if err != nil {
			t.Errorf("parse(%q) failed: %v", tc.src, err)
			continue
		}
		if got := len(prog.Body); got != tc.want {
			t.Errorf("parse(%q): got %d statements, want %d", tc.src, got, tc.want)
		}
	}

	prog := mustParse(t, "function f() { return\na }", script)
	body := prog.Body[0].(*ast.FuncDecl).Func.Body
	if r, ok := body[0].(*ast.ReturnStmt); !ok || r.Arg != nil || len(body) != 2 {
		t.Errorf("return followed by a line break: want a return without an argument and one more statement")
	}
}

func declString(s *ast.Scope) string {
	kinds := [...]string{
		ast.DeclVar:        "var",
		ast.DeclLet:        "let",
		ast.DeclConst:      "const",
		ast.DeclFunction:   "function",
		ast.DeclClass:      "class",
		ast.DeclParam:      "param",
		ast.DeclCatchParam: "catch",
		ast.DeclImport:     "import",
		ast.DeclAnnexBVar:  "annexb",
	}
	var parts []string
	for _, d := range s.Decls {
		parts = append(parts, kinds[d.Kind]+" "+d.Name)
	}
	return strings.Join(parts, ", ")
}

func TestScopes(t *testing.T) {
	prog := mustParse(t, `
var a;
let b;
const c = 1;
function d(p, [q] = [], ...r) {
	var a, e;
	{ var f; let g; function h() {} }
	for (var i of []) { var j; }
	for (let k = 0; ; ) { const l = 1; }
	try {} catch (m) { var n; let o; }
	switch (0) { case 0: let s; var u; }
	let v;
	class W {}
	var a;
	function a() {}
}
class X {}
{ function y() {} }
if (a) function z() {}
`, script)

	if got, want := declString(prog.Scope), "var a, let b, const c, function d, class X, annexb y, annexb z"; got != want {
		t.Errorf("program scope: got %q, want %q", got, want)
	}
	if prog.Scope.Kind != ast.ScopeScript || prog.Scope.Parent != nil {
		t.Errorf("program scope: wrong kind or parent")
	}

	fd := prog.Body[3].(*ast.FuncDecl)
	if fd.AnnexBHoist {
		t.Errorf("d: AnnexBHoist is set for a top level function")
	}
	fn := fd.Func
	if got, want := declString(fn.Scope), "param p, param q, param r, var a, var e, var f, var i, var j, var n, var u, let v, class W, function a, annexb h"; got != want {
		t.Errorf("function scope: got %q, want %q", got, want)
	}
	if fn.Scope.Parent != prog.Scope || fn.Scope.Kind != ast.ScopeFunction {
		t.Errorf("function scope: wrong kind or parent")
	}
	if got, want := fn.Length, 1; got != want {
		t.Errorf("Length: got %d, want %d", got, want)
	}

	block := fn.Body[1].(*ast.BlockStmt)
	if got, want := declString(block.Scope), "let g, function h"; got != want {
		t.Errorf("block scope: got %q, want %q", got, want)
	}
	if !block.Body[2].(*ast.FuncDecl).AnnexBHoist {
		t.Errorf("h: AnnexBHoist is not set")
	}
	forStmt := fn.Body[3].(*ast.ForStmt)
	if got, want := declString(forStmt.Scope), "let k"; got != want {
		t.Errorf("for scope: got %q, want %q", got, want)
	}
	if got, want := declString(forStmt.Body.(*ast.BlockStmt).Scope), "const l"; got != want {
		t.Errorf("for body scope: got %q, want %q", got, want)
	}
	if forStmt.Body.(*ast.BlockStmt).Scope.Parent != forStmt.Scope {
		t.Errorf("for body scope: the parent is not the for scope")
	}
	try := fn.Body[4].(*ast.TryStmt)
	if got, want := declString(try.CatchScope), "catch m"; got != want {
		t.Errorf("catch scope: got %q, want %q", got, want)
	}
	if got, want := declString(try.Handler.Scope), "let o"; got != want {
		t.Errorf("catch body scope: got %q, want %q", got, want)
	}
	if got, want := declString(fn.Body[5].(*ast.SwitchStmt).Scope), "let s"; got != want {
		t.Errorf("switch scope: got %q, want %q", got, want)
	}

	ifStmt := prog.Body[6].(*ast.IfStmt)
	if b, ok := ifStmt.Consequent.(*ast.BlockStmt); !ok {
		t.Errorf("function in an if statement: got %T, want a synthesized *ast.BlockStmt", ifStmt.Consequent)
	} else if got, want := declString(b.Scope), "function z"; got != want {
		t.Errorf("if function scope: got %q, want %q", got, want)
	}
}

func TestAnnexBHoisting(t *testing.T) {
	testCases := []struct {
		src  wtf8.String
		want bool
	}{
		{
			src:  "{ function f() {} }",
			want: true,
		},
		{
			src:  "{ { function f() {} } }",
			want: true,
		},
		{
			src:  "{ function f() {} } var f;",
			want: true,
		},
		{
			src:  "let f; { function f() {} }",
			want: false,
		},
		{
			src:  "{ function f() {} } let f;",
			want: false,
		},
		{
			src:  "{ let f; { function f() {} } }",
			want: false,
		},
		{
			src:  "{ { function f() {} } let f; }",
			want: false,
		},
		{
			src:  "for (let f;;) { function f() {} }",
			want: false,
		},
		{
			src:  "try {} catch (f) { { function f() {} } }",
			want: true,
		},
		{
			src:  "try {} catch ({f}) { { function f() {} } }",
			want: false,
		},
		{
			src:  "switch (0) { case 0: function f() {} }",
			want: true,
		},
		{
			src:  "'use strict'; { function f() {} }",
			want: false,
		},
		{
			src:  "(function (f) { { function f() {} } })",
			want: false,
		},
		{
			src:  "{ function* f() {} }",
			want: false,
		},
	}
	for _, tc := range testCases {
		prog := mustParse(t, tc.src, script)
		var got bool
		var visit func(s *ast.Scope)
		visit = func(s *ast.Scope) {
			for _, d := range s.Decls {
				if d.Kind == ast.DeclAnnexBVar && d.Name == "f" {
					got = true
				}
			}
		}
		visit(prog.Scope)
		if fn, ok := prog.Body[0].(*ast.ExprStmt); ok {
			if f, ok := fn.Expr.(*ast.Function); ok {
				visit(f.Scope)
			}
		}
		if got != tc.want {
			t.Errorf("parse(%q): Annex B hoisting: got %t, want %t", tc.src, got, tc.want)
		}
	}
}

func TestFunctionFacts(t *testing.T) {
	prog := mustParse(t, `
function a() { return () => [this, arguments, new.target]; }
function b() { eval(''); function c() {} }
function d() { 'use strict'; return function () { (0, eval)(''); }; }
class E extends F {
	constructor() { (() => super())(); }
	m() { return super.x; }
	static { this; }
	f = () => this;
	get g() { return 1; }
	async *h() {}
}
var i = (x = eval('')) => x;
var j = (x = function () { eval(''); }) => x;
`, script)

	a := prog.Body[0].(*ast.FuncDecl).Func
	if !a.UsesThis || !a.UsesArguments || !a.UsesNewTarget || a.UsesSuperProperty || a.HasDirectEval || a.Strict {
		t.Errorf("a: wrong facts: %+v", *a)
	}
	arrow := a.Body[0].(*ast.ReturnStmt).Arg.(*ast.Function)
	if !arrow.Arrow || !arrow.UsesThis || !arrow.UsesArguments || !arrow.UsesNewTarget {
		t.Errorf("arrow in a: wrong facts")
	}

	b := prog.Body[1].(*ast.FuncDecl).Func
	if !b.HasDirectEval || !b.Scope.ContainsDirectEval || b.UsesThis {
		t.Errorf("b: wrong facts")
	}
	if c := b.Body[1].(*ast.FuncDecl).Func; c.HasDirectEval || c.Scope.ContainsDirectEval {
		t.Errorf("c: direct eval is reported for a function that has none")
	}
	if !prog.Scope.ContainsDirectEval {
		t.Errorf("program scope: ContainsDirectEval is not set")
	}

	d := prog.Body[2].(*ast.FuncDecl).Func
	if !d.Strict || d.HasDirectEval || d.Scope.ContainsDirectEval {
		t.Errorf("d: wrong facts")
	}
	if inner := d.Body[1].(*ast.ReturnStmt).Arg.(*ast.Function); !inner.Strict || inner.HasDirectEval {
		t.Errorf("function in d: wrong facts")
	}
	if es := d.Body[0].(*ast.ExprStmt); !es.Directive {
		t.Errorf("d: the directive is not marked")
	}

	cls := prog.Body[3].(*ast.ClassDecl).Class
	if cls.Constructor == nil {
		t.Fatalf("E: Constructor is nil")
	}
	if c := cls.Constructor; !c.ClassConstructor || !c.DerivedConstructor || !c.UsesSuperCall || !c.Method || !c.Strict {
		t.Errorf("E constructor: wrong facts")
	}
	kinds := []ast.MemberKind{ast.MemberConstructor, ast.MemberMethod, ast.MemberStaticBlock, ast.MemberField, ast.MemberGetter, ast.MemberMethod}
	if len(cls.Members) != len(kinds) {
		t.Fatalf("E: got %d members, want %d", len(cls.Members), len(kinds))
	}
	for i, k := range kinds {
		if cls.Members[i].Kind != k {
			t.Errorf("E member %d: got kind %d, want %d", i, cls.Members[i].Kind, k)
		}
	}
	if m := cls.Members[1].Value; !m.UsesSuperProperty || m.UsesSuperCall {
		t.Errorf("E.m: wrong facts")
	}
	if sb := cls.Members[2]; !sb.Static || !sb.Value.StaticBlock || !sb.Value.UsesThis {
		t.Errorf("E static block: wrong facts")
	}
	if f := cls.Members[3].Value; !f.FieldInit || !f.UsesThis || f.ExprBody == nil {
		t.Errorf("E.f: wrong facts")
	}
	if g := cls.Members[4].Value; !g.Getter {
		t.Errorf("E.g: Getter is not set")
	}
	if h := cls.Members[5].Value; !h.Async || !h.Generator {
		t.Errorf("E.h: wrong facts")
	}
	if got, want := prog.Text(cls.Members[5].Value), wtf8.String("async *h() {}"); got != want {
		t.Errorf("E.h source text: got %q, want %q", got, want)
	}

	// A direct eval in the parameters of an arrow function belongs to the arrow function.
	i := prog.Body[4].(*ast.VarDecl).Decls[0].Init.(*ast.Function)
	if !i.HasDirectEval || !i.Scope.ContainsDirectEval {
		t.Errorf("i: the direct eval in the parameters is not recorded")
	}

	// A function in the parameters of an arrow function is nested in the scope of the arrow function.
	j := prog.Body[5].(*ast.VarDecl).Decls[0].Init.(*ast.Function)
	inner := j.Params[0].(*ast.AssignPattern).Default.(*ast.Function)
	if inner.Scope.Parent != j.Scope {
		t.Errorf("j: the scope of the function in the parameters is not a child of the arrow function scope")
	}
	if j.HasDirectEval || !j.Scope.ContainsDirectEval || !inner.HasDirectEval {
		t.Errorf("j: wrong direct eval facts")
	}
}

// ownAssigned returns the names that the own code of the function fn of prog assigns to, sorted
// and joined by commas.
func ownAssigned(prog *ast.Program, fn *ast.Function) string {
	var names []string
	for name, orders := range prog.Assigned {
		if slices.Contains(orders, fn.Order) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func TestAssigned(t *testing.T) {
	testCases := []struct {
		name string

		// src declares a function outer. When outer starts with a return statement, the returned
		// function is inner.
		src   wtf8.String
		outer string
		inner string
	}{
		{
			name:  "arrow function head",
			src:   `function outer() { return (a = b = 1, [c = d = 1] = e, {f = g = 1}, ...[h = i = 1]) => j = 1; }`,
			inner: "b,d,g,i,j",
		},
		{
			name:  "async arrow function head",
			src:   `function outer() { return async (a = b = 1) => 0; }`,
			inner: "b",
		},
		{
			name:  "expressions where a pattern may stand",
			src:   `function outer() { (a = 1); async(b = 1); [c = d = 1] = e; f([g = 1], {h: i = 1}); }`,
			outer: "a,b,c,d,g,i",
		},
		{
			name:  "statements",
			src:   `function outer() { a++; --b; for ((c = 1); ; ); for (d in o); for ([e] of o); class C extends [f = 1].constructor {} }`,
			outer: "a,b,c,d,e,f",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog := mustParse(t, tc.src, script)
			outer := prog.Body[0].(*ast.FuncDecl).Func
			if got := ownAssigned(prog, outer); got != tc.outer {
				t.Errorf("outer: got %q, want %q", got, tc.outer)
			}
			if r, ok := outer.Body[0].(*ast.ReturnStmt); ok {
				if got := ownAssigned(prog, r.Arg.(*ast.Function)); got != tc.inner {
					t.Errorf("inner: got %q, want %q", got, tc.inner)
				}
			}
		})
	}

	// At the top level too, the parameters of an arrow function are its own code, and a function
	// in them is nested in it.
	prog := mustParse(t, `(a = () => {}, b = c = 1) => {};`, script)
	arrow := prog.Body[0].(*ast.ExprStmt).Expr.(*ast.Function)
	if got, want := ownAssigned(prog, arrow), "c"; got != want {
		t.Errorf("top-level arrow function: got %q, want %q", got, want)
	}
	param := arrow.Params[0].(*ast.AssignPattern).Default.(*ast.Function)
	if lo, hi := arrow.Order-arrow.Nested, arrow.Order-1; param.Order < lo || param.Order > hi {
		t.Errorf("Order of the function in the parameters: got %d, want from %d to %d", param.Order, lo, hi)
	}
}

func TestModuleDecls(t *testing.T) {
	prog := mustParse(t, "import a, {b as c} from 'm'; import * as n from 'm'; export function d() {} export default function () {} var e; let f;", module)
	if got, want := declString(prog.Scope), "import a, import c, import n, function d, function *default*, var e, let f"; got != want {
		t.Errorf("module scope: got %q, want %q", got, want)
	}
	if !prog.Module || !prog.Strict || prog.Scope.Kind != ast.ScopeModule {
		t.Errorf("module: wrong program facts")
	}
	imp := prog.Body[0].(*ast.ImportDecl)
	if got, want := len(imp.Specifiers), 2; got != want {
		t.Fatalf("len(Specifiers): got %d, want %d", got, want)
	}
	if s := imp.Specifiers[1]; s.Kind != ast.ImportNamed || s.Imported.Name != "b" || s.Local.Name != "c" {
		t.Errorf("import specifier: got %+v", s)
	}
	if imp.Source.Value != "m" {
		t.Errorf("import source: got %q, want %q", imp.Source.Value, "m")
	}
}

func TestOptions(t *testing.T) {
	testCases := []struct {
		src  wtf8.String
		opts parser.Options
		ok   bool
	}{
		{
			src: "super.x",
			opts: parser.Options{
				AllowSuperProperty: true,
			},
			ok: true,
		},
		{
			src: "super()",
			opts: parser.Options{
				AllowSuperProperty: true,
			},
			ok: false,
		},
		{
			src: "super()",
			opts: parser.Options{
				AllowSuperCall: true,
			},
			ok: true,
		},
		{
			src: "() => super()",
			opts: parser.Options{
				AllowSuperCall: true,
			},
			ok: true,
		},
		{
			src: "new.target",
			opts: parser.Options{
				AllowNewTarget: true,
			},
			ok: true,
		},
		{
			src: "arguments",
			opts: parser.Options{
				InClassFieldInit: true,
			},
			ok: false,
		},
		{
			src: "() => arguments",
			opts: parser.Options{
				InClassFieldInit: true,
			},
			ok: false,
		},
		{
			src: "(function () { arguments })",
			opts: parser.Options{
				InClassFieldInit: true,
			},
			ok: true,
		},
		{
			src: "this.#a + (#b in this)",
			opts: parser.Options{
				PrivateNames: []string{"a", "b"},
			},
			ok: true,
		},
		{
			src: "this.#c",
			opts: parser.Options{
				PrivateNames: []string{"a", "b"},
			},
			ok: false,
		},
		{
			src: "class A { m() { this.#a } }",
			opts: parser.Options{
				PrivateNames: []string{"a"},
			},
			ok: true,
		},
		{
			src: "with (a) b",
			opts: parser.Options{
				Strict: true,
			},
			ok: false,
		},
	}
	for _, tc := range testCases {
		_, err := parser.ParseScript(tc.src, tc.opts)
		if got := err == nil; got != tc.ok {
			t.Errorf("ParseScript(%q, %+v): got error %v, want success %t", tc.src, tc.opts, err, tc.ok)
		}
	}
}

func TestParseFunction(t *testing.T) {
	prog, err := parser.ParseFunction("a, b = 1", "return a + b", parser.NormalFunction, parser.Options{})
	if err != nil {
		t.Fatalf("ParseFunction failed: %v", err)
	}
	if got, want := prog.Source, wtf8.String("function anonymous(a, b = 1\n) {\nreturn a + b\n}"); got != want {
		t.Errorf("Source: got %q, want %q", got, want)
	}
	fn, ok := prog.Body[0].(*ast.ExprStmt).Expr.(*ast.Function)
	if !ok {
		t.Fatalf("body: got %T, want *ast.Function", prog.Body[0].(*ast.ExprStmt).Expr)
	}
	if fn.Name == nil || fn.Name.Name != "anonymous" || len(fn.Params) != 2 || fn.Length != 1 {
		t.Errorf("function: wrong shape")
	}

	testCases := []struct {
		params wtf8.String
		body   wtf8.String
		kind   parser.FunctionKind
		ok     bool
	}{
		{
			params: "",
			body:   "",
			kind:   parser.NormalFunction,
			ok:     true,
		},
		{
			params: "a //",
			body:   "",
			kind:   parser.NormalFunction,
			ok:     true,
		},
		{
			params: "",
			body:   "yield 1",
			kind:   parser.GeneratorFunction,
			ok:     true,
		},
		{
			params: "",
			body:   "await 1",
			kind:   parser.AsyncFunction,
			ok:     true,
		},
		{
			params: "",
			body:   "for await (x of yield y);",
			kind:   parser.AsyncGeneratorFunction,
			ok:     true,
		},
		{
			params: "a",
			body:   "'use strict'; return a",
			kind:   parser.NormalFunction,
			ok:     true,
		},
		{
			params: "",
			body:   "yield 1",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: "",
			body:   "await 1",
			kind:   parser.GeneratorFunction,
			ok:     false,
		},
		{
			params: "a = yield",
			body:   "",
			kind:   parser.GeneratorFunction,
			ok:     false,
		},
		{
			params: "a = await 1",
			body:   "",
			kind:   parser.AsyncFunction,
			ok:     false,
		},
		{
			params: "a = 1",
			body:   "'use strict'",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: "a, a",
			body:   "'use strict'",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: "",
			body:   "}); (function () {",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: ") { }, function (",
			body:   "",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: "/*",
			body:   "*/",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: "a",
			body:   "super.x",
			kind:   parser.NormalFunction,
			ok:     false,
		},
		{
			params: "a",
			body:   "let a",
			kind:   parser.NormalFunction,
			ok:     false,
		},
	}
	for _, tc := range testCases {
		_, err := parser.ParseFunction(tc.params, tc.body, tc.kind, parser.Options{})
		if got := err == nil; got != tc.ok {
			t.Errorf("ParseFunction(%q, %q, %d): got error %v, want success %t", tc.params, tc.body, tc.kind, err, tc.ok)
		}
		if err != nil {
			if _, ok := errors.AsType[*parser.Error](err); !ok {
				t.Errorf("ParseFunction(%q, %q, %d): error type is %T, want *parser.Error", tc.params, tc.body, tc.kind, err)
			}
		}
	}
}

func TestPosition(t *testing.T) {
	prog := mustParse(t, "a;\néé; b\r\n  c", script)
	s := prog.Body[2]
	if got, want := prog.Text(s), wtf8.String("b"); got != want {
		t.Errorf("Text: got %q, want %q", got, want)
	}
	lines := srcpos.NewTable(prog.Source)
	if line, col := lines.Position(s.Start()); line != 2 || col != 5 {
		t.Errorf("Position of b: got %d:%d, want 2:5", line, col)
	}
	if line, col := lines.Position(prog.Body[3].Start()); line != 3 || col != 3 {
		t.Errorf("Position of c: got %d:%d, want 3:3", line, col)
	}

	// A lone surrogate, in WTF-8, is one code point, also on a line longer than the blocks
	// that the code points are counted in.
	prog = mustParse(t, wtf8.String("a;\n'\xed\xa0\x80"+strings.Repeat("x", 300)+"'; d"), script)
	if line, col := srcpos.NewTable(prog.Source).Position(prog.Body[2].Start()); line != 2 || col != 306 {
		t.Errorf("Position of d: got %d:%d, want 2:306", line, col)
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		"var a = 1; let [b, {c}] = d; const e = () => { return a ?? b; };",
		"class A extends B { static #x = 1; constructor() { super(); } get y() { return this.#x; } static { A.z = 1; } }",
		"async function* f(a = 1, ...b) { for await (const x of y) yield* x; }",
		"label: for (var i in o) { if (i) continue label; else break; }",
		"x = `a${b}c${`d`}` + /re[/]/g.test(s) / 2",
		"({a = 1, b: [c, ...d], ...e} = f); (g, h) => i; async (j) => k",
		"import a, {b as c} from 'm' with {type: 'json'}; export default class {}; export * as n from 'm';",
		"try { throw 1 } catch ({message}) { switch (message) { case 1: default: } } finally { debugger }",
		"a?.b?.[c]?.(d) ?? new.target; 0x1F + 1_0n + .5e3 + 017 + '\\uD83D\\uDE00'",
		"<!-- c\n--> d\n#!e",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		// Only the absence of a panic is checked. The parser rejects bytes that are not canonical
		// WTF-8, so any bytes are fed.
		src := wtf8.String(text)
		_, _ = parser.ParseScript(src, parser.Options{})
		_, _ = parser.ParseModule(src, parser.Options{})
		_, _ = parser.ParseFunction(src, src, parser.AsyncGeneratorFunction, parser.Options{})
	})
}

// TestListCapacity checks that lists have no spare capacity, so that appending to one cannot
// change another list that shares its allocation.
func TestListCapacity(t *testing.T) {
	prog := mustParse(t, "f(1, 2);\ng([3, 4], 5);\n", script)
	f := prog.Body[0].(*ast.ExprStmt).Expr.(*ast.CallExpr)
	g := prog.Body[1].(*ast.ExprStmt).Expr.(*ast.CallExpr)
	arr := g.Args[0].(*ast.ArrayLit)
	for _, l := range []struct {
		name     string
		len, cap int
	}{
		{
			name: "statements",
			len:  len(prog.Body),
			cap:  cap(prog.Body),
		},
		{
			name: "arguments of f",
			len:  len(f.Args),
			cap:  cap(f.Args),
		},
		{
			name: "arguments of g",
			len:  len(g.Args),
			cap:  cap(g.Args),
		},
		{
			name: "elements",
			len:  len(arr.Elements),
			cap:  cap(arr.Elements),
		},
	} {
		if l.cap != l.len {
			t.Errorf("%s: got capacity %d, want %d", l.name, l.cap, l.len)
		}
	}
}
