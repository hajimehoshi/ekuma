// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// The operations that can stand for calls of built-in functions without a kernel or a step, which
// run after kernel-probes.js. In a realm where generic is set, the built-in functions are proxies
// of themselves, which show no frames, so that the operations at the same positions call them.

var originals = new Map;
if (generic) {
  var replace = function (o, key) {
    var d = Object.getOwnPropertyDescriptor(o, key);
    for (var field of ["value", "get"]) {
      if (typeof d[field] !== "function") continue;
      var p = new Proxy(d[field], {});
      originals.set(p, d[field]);
      d[field] = p;
    }
    Object.defineProperty(o, key, d);
  };
  for (var [o, keys] of [
    [Function.prototype, ["call", "apply"]],
    [Array, [Symbol.species]],
    [Object.getPrototypeOf(Uint8Array.prototype), ["length"]],
    [Map.prototype, ["set"]],
    [Set.prototype, ["add", "has"]],
    [WeakMap.prototype, ["set"]],
    [WeakSet.prototype, ["add"]],
    [RegExp, [Symbol.species]],
    [RegExp.prototype, ["exec", "flags", "hasIndices", "global", "ignoreCase", "multiline", "dotAll", "unicode", "unicodeSets", "sticky"]],
    [Promise, [Symbol.species]],
    [Promise.prototype, ["constructor"]],
    [Object.getPrototypeOf(function* () {}).prototype, ["next"]],
    [Object.getPrototypeOf([].values()), ["next"]],
    [Array.prototype, [Symbol.iterator]],
  ]) keys.forEach(function (key) { replace(o, key); });
  Promise = Promise.prototype.constructor;
}

// plain returns the object o, or a proxy of it that makes every operation on it generic.
function plain(o) {
  return generic ? new Proxy(o, {}) : o;
}

var describeTrace = trace;
trace = function (e) {
  var watching = setWatching(false);
  try {
    return describeTrace(e);
  } finally {
    setWatching(watching);
  }
};

var undo = [];

// define defines a property that is restored after the probe.
function define(o, key, d) {
  var old = Object.getOwnPropertyDescriptor(o, key);
  undo.push(function () {
    if (old) Object.defineProperty(o, key, old); else delete o[key];
  });
  d.configurable = true;
  Object.defineProperty(o, key, d);
}

// newLog returns a log that the properties of the prototypes have no effect on.
function newLog() {
  return Object.defineProperty([], "push", {value() {
    for (var i = 0; i < arguments.length; i++) {
      Object.defineProperty(this, this.length, {value: arguments[i], writable: true, enumerable: true, configurable: true});
    }
    return this.length;
  }});
}

// The index of the elements that arrays lack.
var hole = 40;

function withHole(get) {
  var a = range(hole + 2);
  delete a[hole];
  define(Array.prototype, hole, {get});
  return a;
}

// withoutElement returns an array that stores no element at the hole.
function withoutElement(get) {
  var a = range(hole);
  a.length = hole + 1;
  define(Array.prototype, hole, {get});
  return a;
}

function sparse(get) {
  return Object.defineProperty(range(hole + 2), hole, {get, configurable: true});
}

function thrower() {
  throw new EvalError;
}

var arrayIteratorPrototype = Object.getPrototypeOf([].values());

// iterables are the arrays that the operations iterate without an iterator.
var iterables = {
  "array": () => [1, "b", shared],
  "empty array": () => [],
  "array of pairs": () => [[1, 2], ["a", "b"]],
  "array of no pairs": () => [[1], [], [, 2], "ab", new Set([1, 2]), [1, 2, 3]],
  "number": () => [1],
  "hole": () => withHole(hook("hole", [1, 2])),
  "hole that throws": () => withHole(thrower),
  "missing element": () => withoutElement(hook("missing", [1, 2])),
  "accessor": () => sparse(hook("accessor", [1, 2])),
  "hole that empties the array": () => {
    var a = withHole(function () { hook("empties")(); a.length = 0; return [1, 2]; });
    return a;
  },
  "hole that extends the array": () => {
    var a = withHole(function () { hook("extends")(); a.length < 50 && a.push([3, 4]); return [1, 2]; });
    return a;
  },
  "hole that modifies the iteration": () => withHole(function () {
    hook("modifies")();
    define(arrayIteratorPrototype, "next", {value: thrower});
    define(Array.prototype, Symbol.iterator, {value: thrower});
    return 1;
  }),
  "renamed iterator": () => {
    define(arrayIteratorPrototype, Symbol.toStringTag, {value: "Renamed"});
    return withHole(hook("hole", [1, 2]));
  },
};
for (let [key, d] of [
  ["method", {value: hook("return", {})}],
  ["method of no object", {value: hook("return", 1)}],
  ["throwing method", {value: thrower}],
  ["method that continues", {value: function () {
    hook("return")();
    var watching = setWatching(false);
    log.push(JSON.stringify(this.next()));
    setWatching(watching);
    return this;
  }}],
  ["getter", {get: hook("get return")}],
  ["throwing getter", {get: thrower}],
  ["value", {value: 1}],
]) {
  iterables["return " + key] = () => {
    define(arrayIteratorPrototype, "return", d);
    return [[1, 2], [3, 4]];
  };
}
iterables["return method after a hole"] = () => {
  define(arrayIteratorPrototype, "return", {value: hook("return", {})});
  return withHole(hook("hole", [1, 2]));
};
iterables["inherited return getter"] = () => {
  define(Object.prototype, "return", {get: hook("get return")});
  return [[1, 2], [3, 4]];
};

var arrayLikeSources = () => [Array.prototype[Symbol.iterator], arrayIteratorPrototype.next];

function* delegating() {
  yield* iterator({next() { return getter(getter({}, "value", 1), "done", log.length > 8); }, return: hook("return", {})});
}

var generators = {
  "values": () => (function* g() { yield [1, 2]; hook("body")(); yield [3, 4]; })(),
  "observed start": () => (function* g() { hook("body")(); })(),
  "closed": () => (function* g() { try { yield 1; yield 2; } finally { hook("finally")(); } })(),
  "throwing": () => (function* g() { yield 1; throw new EvalError; })(),
  "throwing close": () => (function* g() { try { yield 1; } finally { throw new EvalError; } })(),
  "finished": () => { var g = (function* () {})(); g.next(); return g; },
  "running": () => { var g = (function* () { yield* g; })(); return g; },
  "delegating": () => delegating(),
  "delegating to a result of no object": () => (function* () { yield* iterator({next() { return 1; }}); })(),
};

function regexps(strings) {
  var out = {};
  for (let [key, f] of Object.entries(strings)) {
    out[key] = f;
  }
  for (let flags of ["", "g", "y", "gy", "u", "gi", "dgv"]) {
    out["flags " + flags] = () => [new RegExp("(a)|(?<n>x?)", flags), "bab\u{1d306}aa"];
    out["last index of flags " + flags] = () => {
      var r = new RegExp("(a)|(?<n>x?)", flags);
      r.lastIndex = 2;
      return [r, "bab\u{1d306}aa"];
    };
    out["observed last index of flags " + flags] = () => {
      var r = new RegExp("a", flags);
      r.lastIndex = {valueOf: hook("lastIndex", 1)};
      return [r, "aaa"];
    };
    out["recompiled by the last index of flags " + flags] = () => {
      var r = new RegExp("a", flags);
      r.lastIndex = {valueOf() { hook("lastIndex")(); r.compile("(b)", flags); return 0; }};
      return [r, "aab"];
    };
    out["read-only last index of flags " + flags] = () => [Object.freeze(new RegExp("a", flags)), "aaa"];
    out["throwing last index of flags " + flags] = () => {
      var r = new RegExp("a", flags);
      r.lastIndex = {valueOf: thrower};
      return [r, "aaa"];
    };
  }
  return out;
}

var regexpOperands = regexps({
  "no match": () => [/x/g, "aaa"],
  "empty string": () => [/(?:)/g, ""],
  "empty matches": () => [/(?:)/gu, "\u{1d306}a"],
  "converting string": () => [/a/g, {toString: hook("toString", "aba")}],
  "observed flags": () => [getter(/a/g, "flags", "g"), "aba"],
  "observed flag": () => [getter(/a/g, "global", true), "aba"],
  "observed exec": () => [getter(/a/g, "exec", hook("exec", null)), "aba"],
  "observed constructor": () => [getter(/a/g, "constructor", RegExp), "aba"],
  "observed species": () => [Object.assign(/a/g, {constructor: getter({}, Symbol.species, undefined)}), "aba"],
});

// matched describes what a match leaves behind.
function matched(r, v) {
  return JSON.stringify([v, r.lastIndex, RegExp.lastMatch, RegExp.lastParen, RegExp.leftContext, RegExp.rightContext, RegExp.input, RegExp.$1, RegExp.$2]);
}

var sets = {
  "subset": () => [new Set([1, "a"]), new Set([1, "a", NaN, shared])],
  "disjoint sets": () => [new Set([1, 2]), new Set([3, 4])],
  "modified set": () => {
    var s = new Set([1, 2, 3]);
    return [s, getter(new Set([1, 2, 3, 4]), "keys", Set.prototype.keys)];
  },
  "set cleared by the size": () => {
    var s = new Set([1, 2]);
    return [s, Object.defineProperty(new Set([1, 2, 3]), "size", {get() { hook("size")(); s.clear(); s.add(3); return 3; }})];
  },
  "throwing size": () => [new Set([1]), Object.defineProperty(new Set([1]), "size", {value: {valueOf: thrower}})],
};

// arrays are the arrays whose elements the methods read and write at once.
var arrays = {
  "array": () => [3, 1, 2],
  "empty array": () => [],
  "strings": () => ["b", "a", "c"],
  "strings of other text": () => ["é", "a"],
  "undefined and null": () => [undefined, null, 1, undefined],
  "objects": () => [{toString: hook("toString", "x")}, {valueOf: hook("valueOf", 1)}, shared],
  "throwing elements": () => [1, {toString: thrower, valueOf: thrower}],
  "nested arrays": () => [[1, [2]], [], [[3]]],
  "hole": () => [1, , 3],
  "trailing holes": () => { var a = [1]; a.length = 3; return a; },
  "inherited element": () => withHole(hook("hole", 5)),
  "missing inherited element": () => withoutElement(hook("missing", 5)),
  "accessor": () => sparse(hook("accessor", 5)),
  "read-only element": () => Object.defineProperty([1, 2, 3], 1, {value: 2, writable: false}),
  "frozen array": () => Object.freeze([2, 1]),
  "sealed array": () => Object.seal([2, 1]),
  "array of no extensions": () => Object.preventExtensions([2, 1]),
  "frozen length": () => Object.defineProperty([2, 1], "length", {writable: false}),
  "inherited setter": () => { define(Array.prototype, hole, {set: hook("set"), get: hook("get", 1)}); return range(hole); },
  "observed constructor": () => getter([1, 2], "constructor", Array),
  "constructor of another kind": () => Object.assign([1, 2], {constructor: {[Symbol.species]: function Species(n) { hook("species")(); return {length: n}; }}}),
  "observed species": () => {
    var a = [1, 2];
    a.constructor = getter({}, Symbol.species, undefined);
    return a;
  },
};

var arrayMethods = {
  at: a => a.at(-1),
  concat: a => a.concat(a, 1, [2, 3]),
  copyWithin: a => a.copyWithin(1, 0),
  fill: a => a.fill(9, 1),
  flat: a => a.flat(2),
  includes: a => [a.includes(2), a.includes(undefined), a.includes(NaN, 1)],
  indexOf: a => [a.indexOf(2), a.indexOf(undefined), a.indexOf(1, -1)],
  join: a => a.join("-"),
  lastIndexOf: a => [a.lastIndexOf(2), a.lastIndexOf(undefined), a.lastIndexOf(1, -2)],
  pop: a => [a.pop(), a.pop()],
  push: a => a.push(8, 9),
  reverse: a => a.reverse(),
  shift: a => [a.shift(), a.shift()],
  slice: a => a.slice(1),
  sort: a => a.sort(),
  splice: a => a.splice(1, 1, "x", "y"),
  toReversed: a => a.toReversed(),
  toSorted: a => a.toSorted(),
  toSpliced: a => a.toSpliced(0, 1, "x"),
  unshift: a => a.unshift(8, 9),
  with: a => a.with(0, 7),
  map: a => a.map(hook("mapper", 1)),
  filter: a => a.filter(hook("predicate", true)),
  forEach: a => a.forEach(hook("callback")),
  reduceRight: a => a.reduceRight(hook("reducer", 1)),
  find: a => a.find(hook("predicate", false)),
  "sort by a function": a => a.sort(hook("comparison", -1)),
  // The conversions of the arguments change the array after its length is read.
  "fill after emptying": a => a.fill(9, {valueOf() { hook("start")(); a.length = 0; return 0; }}),
  "slice after emptying": a => a.slice({valueOf() { hook("start")(); a.length = 0; return 0; }}),
  "slice after extending": a => a.slice({valueOf() { hook("start")(); a.length < 50 && a.push(7, 8); return 0; }}),
  "splice after emptying": a => a.splice({valueOf() { hook("start")(); a.length = 0; return 0; }}, 2, 1),
  "includes after emptying": a => a.includes(undefined, {valueOf() { hook("start")(); a.length = 0; return 0; }}),
  "lastIndexOf after emptying": a => a.lastIndexOf(3, {valueOf() { hook("start")(); a.length = 0; return 9; }}),
  "join after emptying": a => a.join({toString() { hook("separator")(); a.length = 0; return "-"; }}),
  "copyWithin after emptying": a => a.copyWithin({valueOf() { hook("target")(); a.length = 0; return 0; }}, 1),
  "concat after emptying": a => a.concat(Object.defineProperty([1], Symbol.isConcatSpreadable, {get() { hook("spreadable")(); a.length = 0; return true; }}), a),
};

var sources = {
  "data": () => ({b: 1, a: 2, [symbol]: 3}),
  "string keys": () => ({b: 1, a: 2}),
  "elements": () => ({b: 1, 3: 2, 1: 3}),
  "dense elements": () => ({b: 1, 1: 2, 0: 3}),
  "array": () => [1, , 3],
  "accessors": () => ({get a() { hook("get a")(); return 1; }, b: 2}),
  "accessor that changes the source": () => ({get a() { hook("get a")(); delete this.b; this.c = 1; return 1; }, b: 2}),
  "throwing accessor": () => ({a: 1, get b() { throw new EvalError; }, c: 2}),
  "hidden property": () => Object.defineProperty({a: 1}, "b", {value: 2}),
  "string": () => "ab",
  "String object": () => Object.assign(new String("ab"), {c: 1}),
  "null": () => null,
  "function": () => Object.assign(function () {}, {a: 1}),
  "observing proxy": () => proxy({a: 1, b: 2}),
  "inherited setter": () => { define(Object.prototype, "a", {set: hook("set a")}); return {__proto__: null, a: 1}; },
  "inherited setter of data": () => { define(Object.prototype, "a", {set: hook("set a")}); return {b: 1, a: 2}; },
  "inherited read-only property": () => { define(Object.prototype, "a", {value: 1, writable: false}); return {__proto__: null, b: 1, a: 2}; },
};

function callee(f) {
  return () => f;
}

var functions = {
  "observer": () => hook("function", 1),
  "strict function": callee(function () { "use strict"; return [this === undefined || typeof this, ...arguments]; }),
  "strict function of parameters": callee(function (a, b) { "use strict"; hook("inner")(); return [a, b]; }),
  "sloppy function": callee(function () { return [typeof this, arguments.length]; }),
  "function of a property": callee(function () { return this.p; }),
  "strict function of a property": callee(function () { "use strict"; return this.p; }),
  "throwing function": callee(thrower),
  "generator function": callee(function* () { hook("body")(); }),
  "async function": callee(async function () { hook("body")(); }),
  "class": callee(class C {}),
  "native function": callee(Math.max),
  "native function that calls": callee(Array.prototype.map.bind([1], hook("mapper"))),
  "native function that throws": callee(Symbol.prototype.valueOf),
  "bound function": () => hook("function", 1).bind({p: 1}, 2),
  "bound function without arguments": () => hook("function", 1).bind({p: 1}),
  "bound bound class": callee(class C {}.bind().bind()),
  "bound throwing native function": callee(Array.prototype.map.bind(null)),
  "proxy": () => proxy(function f() { return 1; }),
  "proxy without traps": () => new Proxy(hook("function", 1), {}),
  "call": callee(Function.prototype.call),
  "apply": callee(Function.prototype.apply),
};
var callable = {...functions};
functions.object = () => ({__proto__: Function.prototype});

var lists = {
  "array": () => [1, 2],
  "empty array": () => [],
  "undefined": () => undefined,
  "null": () => null,
  "number": () => 1,
  "string": () => "ab",
  "symbol": () => symbol,
  "object": () => ({length: 2, 0: 1}),
  "observed object": () => getter(getter({}, "length", 1), 0, 1),
  "hole": () => withHole(hook("hole", 1)),
  "proxy": () => proxy([1, 2]),
  "arguments": () => (function () { return arguments; })(1, 2),
  "typed array": () => new Uint8Array([1, 2]),
};

function pairsOf(a, b) {
  var out = {};
  for (let x in a) for (let y in b) out[x + " with " + y] = () => [a[x](), b[y]()];
  return out;
}

var forwarded = pairsOf(functions, {"list": () => [1, 2], "no list": () => []});
var applied = pairsOf({"observer": functions.observer, "strict function": functions["strict function"], "object": functions.object, "native function": functions["native function"]}, lists);
var thenables = {
  "value": () => 1,
  "object": () => ({}),
  "thenable": () => ({then(f) { hook("then")(); f(1); }}),
  "throwing thenable": () => ({then: thrower}),
  "observed then": () => getter({}, "then", 1),
  "inherited observed then": () => { define(Object.prototype, "then", {get: hook("get then")}); return {}; },
  "observed then of arrays": () => { define(Array.prototype, "then", {get: hook("get then")}); return []; },
  "throwing then": () => Object.defineProperty({}, "then", {get: thrower}),
  "observing proxy": () => proxy({}),
  "promise": () => Promise.resolve(1),
  "rejected promise": () => Promise.reject(shared),
  "promise with an observed constructor": () => getter(Promise.resolve(1), "constructor", Promise),
  "promise with another constructor": () => Object.assign(Promise.resolve(1), {constructor: function () {}}),
  "promise with an observed then": () => getter(Promise.resolve(1), "then", Promise.prototype.then),
};

// The probes: sites are the functions with the operation, operands what they get, and watch
// returns the functions that the operation stands for. All operands but those of declined take
// the shortcut.
var probes = {
  call: {
    watch: () => [Function.prototype.call],
    operands: functions,
    declined: / of (?:call|object)$/,
    sites: {
      none: f => f.call(),
      this: function site(f) { return f.call({p: 1}); },
      arguments: function site(f) { return f.call(null, {valueOf: hook("valueOf", 1)}, 2); },
      tail: function (f) { "use strict"; return f.call({p: 1}, 2); },
    },
  },
  "spread call": {
    watch: () => [Function.prototype.call],
    operands: functions,
    declined: / of (?:call|object)$/,
    sites: {tail: function (f) { "use strict"; return f.call(...[{p: 1}, 2]); }},
  },
  "apply of arguments": {
    watch: () => [Function.prototype.apply],
    operands: forwarded,
    declined: / of apply/,
    sites: {
      call: function ([f, list]) {
        function site() { return f.apply(this, arguments); }
        running = site;
        return site(...list);
      },
      "call with a parameter": function ([f, list]) {
        function site(a) { return f.apply(this, arguments); }
        running = site;
        return site(...list);
      },
      tail: function ([f, list]) {
        function site() { "use strict"; return f.apply(this, arguments); }
        return site(...list);
      },
    },
  },
  apply: {
    watch: () => [Function.prototype.apply],
    operands: applied,
    declined: / of object|(?:string|symbol|number|object|hole|proxy|arguments|typed array)$/,
    sites: {tail: function (v) { "use strict"; return v[0].apply({p: 1}, v[1]); }},
  },
  "bound function": {
    watch: () => [],
    operands: callable,
    prepare: f => plain(Function.prototype.bind.call(f, {p: 1})),
    sites: {
      call: function site(f) { return f(2); },
      method: function site(f) { return {f}.f(2, 3); },
      spread: function site(f) { return f(...[2]); },
      tail: function (f) { "use strict"; return f(2); },
      callback: function site(f) { return [1].map(f); },
    },
  },
  "bound arguments": {
    watch: () => [],
    operands: callable,
    prepare: f => plain(Function.prototype.bind.call(f, {p: 1}, 3, 4).bind(null, 5)),
    skipped: /^construction of proxy$/,
    sites: {
      call: function site(f) { return f(2); },
      tail: function (f) { "use strict"; return f(2); },
      construction: function site(f) { return new f(2); },
    },
  },
  iteration: {
    watch: arrayLikeSources,
    operands: iterables,
    declined: /accessor$|^yield star/,
    sites: {
      "spread": function site(v) { return [...v]; },
      "spread call": function site(v) { return String.raw({raw: "ab"}, ...v); },
      "spread construction": function site(v) { return new Array(...v, 1); },
      "for of": function site(v) { for (var x of v) log.push(name(x, [])); },
      "for of with a break": function site(v) { for (var x of v) { log.push(name(x, [])); break; } },
      "for of with a throw": function site(v) { for (var x of v) throw new RangeError; },
      "for of with a return": function site(v) { for (var x of v) return x; },
      "destructuring": function site(v) { var [x, y] = v; return [x, y]; },
      "destructuring with a rest": function site(v) { var [x, ...y] = v; return [x, y]; },
      "destructuring all": function site(v) { var [, , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , , x, , , , , , , , , , , , y] = v; return [x, y]; },
      "pairs": function site(v) { for (var [x, y] of v) log.push(name(x, []), name(y, [])); },
      "pairs with a break": function site(v) { for (var [x, y] of v) { log.push(name(x, []), name(y, [])); break; } },
      "yield star": function site(v) { return (function* () { yield* v; })().toArray(); },
      "Array.from": function site(v) { return Array.from(v); },
      "Array.from with a function": function site(v) { return Array.from(v, hook("mapper")); },
      "Set": function site(v) { return new Set(v); },
      "Map": function site(v) { return new Map(v); },
      "WeakSet": function site(v) { return new WeakSet(v); },
      "Object.fromEntries": function site(v) { return Object.fromEntries(v); },
      "Object.groupBy": function site(v) { return Object.groupBy(v, hook("group", "k")); },
      "typed array": function site(v) { return new Uint8Array(v); },
    },
  },
  generators: {
    watch: () => [Object.getPrototypeOf(function* () {}).prototype.next],
    operands: generators,
    declined: / running$/,
    sites: {
      "spread": function site(g) { return [...g]; },
      "for of with a break": function site(g) { for (var x of g) break; },
      "destructuring": function site(g) { var [x] = g; return x; },
      "pairs": function site(g) { for (var [x, y] of g) log.push(name(x, [])); },
      "yield star": function site(g) { return [...iterator({next() { var [x] = g; return {value: x, done: log.push("inner") > 1}; }})]; },
      "Array.from": function site(g) { return Array.from(g, hook("mapper")); },
      "Map": function site(g) { return new Map(g); },
      "helper": function site(g) { return Iterator.prototype.toArray.call(g); },
    },
  },
  "array species": {
    watch: () => [Object.getOwnPropertyDescriptor(Array, Symbol.species).get],
    operands: {array: arrays.array, strings: arrays.strings, hole: arrays.hole, "inherited element": arrays["inherited element"]},
    sites: {
      concat: a => a.concat([1]),
      filter: a => a.filter(hook("predicate", true)),
      flat: a => a.flat(),
      flatMap: a => a.flatMap(hook("mapper", [1])),
      map: a => a.map(hook("mapper", 1)),
      slice: a => a.slice(1),
      splice: a => a.splice(0, 1),
    },
  },
  "typed array length": {
    watch: () => [Object.getOwnPropertyDescriptor(Object.getPrototypeOf(Uint8Array.prototype), "length").get],
    operands: {
      "array": () => new Uint8Array(3),
      "empty": () => new Float64Array(0),
      "detached": () => { var t = new Uint8Array(3); t.buffer.transfer(); return t; },
      "offset": () => new Int16Array(new ArrayBuffer(8), 2, 2),
      "shared": () => new Int16Array(new SharedArrayBuffer(8)),
      "tracking": () => { var b = new ArrayBuffer(8, {maxByteLength: 16}), t = new Int32Array(b); b.resize(15); return t; },
      "out of bounds": () => { var b = new ArrayBuffer(8, {maxByteLength: 16}), t = new Int32Array(b, 4, 1); b.resize(4); return t; },
      "back in bounds": () => { var b = new ArrayBuffer(8, {maxByteLength: 16}), t = new Int32Array(b, 4, 1); b.resize(4); b.resize(8); return t; },
    },
    // The site is warm.
    prepare: t => {
      for (var i = 0; i < 3; i++) lengthOf(new t.constructor(1));
      return t;
    },
    sites: {length: function site(t) { return lengthOf(t); }},
  },
  Map: {
    watch: () => [Map.prototype.set],
    operands: {
      "pairs": () => [[1, 2], [-0, shared], [0, 1], [1, 3], [NaN, 1], [NaN, 2]],
      "Map": () => new Map([[1, 2], [3, 4]]),
      "generator": generators.values,
      "observed pair": () => [getter(getter([], 0, 1), 1, 2), getter({}, 1, 3)],
      "pair that throws": () => [[1, 2], Object.defineProperty([], 1, {get: thrower})],
      "no pair": () => [[1, 2], 1],
      "pair that modifies set": () => [[1, 2], getter([3], 1, 4), {0: 5, get 1() { define(Map.prototype, "set", {value: thrower}); return 6; }}, [7, 8]],
    },
    sites: {Map: function site(v) { return new Map(v); }},
  },
  Set: {
    watch: () => [Set.prototype.add],
    operands: {
      "values": () => [1, -0, 0, NaN, NaN, "a", shared, shared],
      "Set": () => new Set([1, 2]),
      "generator": generators.values,
      "throwing generator": generators.throwing,
      "hole": () => withHole(hook("hole", 1)),
    },
    sites: {Set: function site(v) { return new Set(v); }},
  },
  WeakMap: {
    watch: () => [WeakMap.prototype.set],
    operands: {
      "pairs": () => [[shared, 1], [symbol, 2], [shared, 3]],
      "invalid key": () => [[shared, 1], [1, 2]],
      "registered symbol": () => [[Symbol.for("s"), 2]],
      "no pair": () => [[shared, 1], 1],
      "invalid key and a return method": () => { define(arrayIteratorPrototype, "return", {value: hook("return", {})}); return [[1, 1]]; },
    },
    declined: /invalid|registered/,
    sites: {WeakMap: function site(v) { var m = new WeakMap(v); return [m.get(shared), m.get(symbol)]; }},
  },
  WeakSet: {
    watch: () => [WeakSet.prototype.add],
    operands: {
      "values": () => [shared, symbol, shared],
      "invalid value": () => [shared, 1],
      "generator": generators.values,
      "generator of an invalid value": generators.closed,
    },
    declined: /invalid/,
    sites: {WeakSet: function site(v) { var s = new WeakSet(v); return [s.has(shared), s.has(symbol)]; }},
  },
  "Set methods": {
    watch: () => [Set.prototype.has],
    operands: sets,
    declined: /throwing size/,
    sites: {
      intersection: ([s, t]) => [...s.intersection(t)],
      difference: ([s, t]) => [...s.difference(t)],
      isSubsetOf: ([s, t]) => s.isSubsetOf(t),
      isDisjointFrom: ([s, t]) => s.isDisjointFrom(t),
    },
  },
  RegExp: {
    watch: () => [RegExp.prototype.exec],
    operands: regexpOperands,
    declined: /observed|last index|converting|modifies/,
    sites: {
      test: function site([r, s]) { return matched(r, r.test(s)); },
      "global match": function site([r, s]) { return matched(r, String.prototype.match.call(s, r.global ? r : /a/g)); },
      replace: function site([r, s]) { return matched(r, String.prototype.replace.call(s, r, "[$&$1$<n>]")); },
      "replace by a function": function site([r, s]) { return matched(r, String.prototype.replace.call(s, r, hook("replacer", "-"))); },
      "replace by a function that recompiles": function site([r, s]) { return matched(r, String.prototype.replace.call(s, r, function () { r.compile("b"); return arguments.length; })); },
      search: function site([r, s]) { return matched(r, String.prototype.search.call(s, r)); },
      split: function site([r, s]) { return matched(r, String.prototype.split.call(s, r)); },
      "split with a limit": function site([r, s]) { return matched(r, String.prototype.split.call(s, r, {valueOf: hook("limit", 3)})); },
      "split with a limit that modifies": function site([r, s]) {
        return matched(r, String.prototype.split.call(s, r, {valueOf() { hook("limit")(); define(RegExp.prototype, "exec", {value: hook("exec", null)}); return 3; }}));
      },
    },
  },
  "RegExp flags": {
    watch: () => ["flags", "global", "sticky"].map(key => Object.getOwnPropertyDescriptor(RegExp.prototype, key).get),
    operands: regexpOperands,
    declined: /observed (?:flags?|exec|constructor|species)|converting|read-only/,
    sites: {
      match: function site([r, s]) { return matched(r, String.prototype.match.call(s, r)); },
      matchAll: function site([r, s]) { return matched(r, [...String.prototype.matchAll.call(s, r)]); },
      "Symbol.matchAll": function site([r, s]) { return matched(r, [...r[Symbol.matchAll](s)]); },
      replaceAll: function site([r, s]) { return matched(r, String.prototype.replaceAll.call(s, r, "-")); },
    },
  },
  "RegExp construction": {
    watch: () => [RegExp.prototype.constructor, Object.getOwnPropertyDescriptor(RegExp, Symbol.species).get],
    operands: regexpOperands,
    declined: /observed (?:flags?|exec|constructor|species)|converting|read-only/,
    sites: {
      "Symbol.matchAll": function site([r, s]) { return matched(r, [...r[Symbol.matchAll](s)]); },
      split: function site([r, s]) { return matched(r, r[Symbol.split](s)); },
    },
  },
  Promise: {
    watch: () => [Promise.prototype.constructor],
    operands: thenables,
    // try calls nothing for a promise of the constructor.
    declined: /another constructor|^try of .*promise/,
    settles: true,
    sites: {
      all: v => Promise.all([v, 1]),
      "all of nothing": v => Promise.all([]),
      allSettled: v => Promise.allSettled([v]),
      "allSettled of nothing": v => Promise.allSettled([]),
      any: v => Promise.any([v]),
      race: v => Promise.race([v]),
      reject: v => Promise.reject(v),
      try: v => Promise.try(() => v),
      "throwing try": v => Promise.try(thrower),
      withResolvers: v => { var r = Promise.withResolvers(); r.resolve(v); return r.promise; },
      then: v => Promise.withResolvers.call(Promise).promise.then(1) && Promise.reject(1).then(1, () => v),
      finally: v => Promise.reject(1).finally(() => v),
    },
  },
  elements: {
    watch: () => [],
    operands: arrays,
    sites: arrayMethods,
    prepare: plain,
  },
  "Object functions": {
    watch: () => [],
    operands: sources,
    prepare: v => v === null || typeof v !== "object" || v instanceof String ? v : plain(v),
    sites: {
      assign: v => Object.assign({}, v),
      "assign to an object": v => Object.assign({z: 1, a: 0}, v),
      keys: v => Object.keys(v),
      values: v => Object.values(v),
      entries: v => Object.entries(v),
      spread: v => ({...v}),
      rest: v => { var {a, ...rest} = v; return rest; },
      "for in": v => { var out = []; for (var key in v) out.push(key); return out; },
      stringify: v => JSON.stringify(v),
    },
  },
};

function lengthOf(t) {
  return t.length;
}

// shortcutProbe runs a site of a probe with an operand.
var outcome, probed, returned;
function shortcutProbe(probeName, siteName, key) {
  var p = probes[probeName], site = p.sites[siteName];
  shared = {name: "shared"};
  undo = [];
  log = newLog();
  outcome = "returned ";
  /a(b)/.exec("xabx");
  probed = p.operands[key]();
  var v = p.prepare ? p.prepare(probed) : probed;
  running = site;
  setWatching(true);
  try {
    returned = site(v);
    setWatching(false);
  } catch (e) {
    setWatching(false);
    outcome = "threw ";
    returned = e;
  }
  running = null;
  if (p.settles) returned.then(v => { log.push("fulfilled " + describe(v, 0)); }, e => { log.push("rejected " + describe(e, 0)); });
}

// settled returns a description of what the last probe did, including the jobs that have run.
function settled() {
  for (var i = undo.length - 1; i >= 0; i--) undo[i]();
  return outcome + describe(returned, 0) + "\n" + log.join("\n") + "\n" + describe(probed, 0);
}

// watched returns the functions that a probe stands for.
function watched(probeName) {
  return probes[probeName].watch().map(f => originals.get(f) || f);
}

function describe(v, depth) {
  if (proxies.has(v)) return "proxy";
  if (Object(v) !== v || v instanceof Error || depth > 3 || v === shared) return name(v, []);
  return name(v, []) + contents(v, []) + Object.prototype.toString.call(v) + Reflect.ownKeys(v).filter(k => k !== "constructor" && k !== "prototype").slice(0, 60).map(function (k) {
    var d = Object.getOwnPropertyDescriptor(v, k);
    return "value" in d && typeof d.value === "object" ? describe(d.value, depth + 1) : "";
  }).join("");
}

// The operations that start with an interruption pending. None may run a function after that.
var started;
function ran() {
  started = true;
}

function* unstarted() {
  started = true;
}

// raising makes the read of the next method of the iterator it request the interruption.
function raising(raise, it) {
  var next = it.next;
  return Object.defineProperty(it, "next", {get() { raise(); return next; }});
}

var pending = {
  "cached instanceof": raise => {
    function F() {}
    for (let i = 0; i < 2; i++) {
      if (i) raise();
      ({}) instanceof F;
      if (i) started = true;
    }
  },
  call: raise => ran.call((raise(), null)),
  "call of no this value": raise => (raise(), ran).call(),
  "call of a native function": raise => Array.prototype.forEach.call((raise(), [1]), ran),
  "apply of arguments": raise => (function () { return ran.apply((raise(), this), arguments); })(1),
  "apply in tail position": raise => (function () { "use strict"; return ran.apply((raise(), null), [1]); })(),
  "call in tail position": raise => (function () { "use strict"; return ran.call((raise(), null), 1); })(),
  "bound function": raise => ran.bind()((raise(), 1)),
  "bound arguments": raise => ran.bind(null, 1)((raise(), 1)),
  "bound function in tail position": raise => (function (f) { "use strict"; return f((raise(), 1)); })(ran.bind()),
  "spread call": raise => ran(...(raise(), [1])),
  "spread of a generator": raise => [...raising(raise, unstarted())],
  "destructuring of a generator": raise => { var [x] = raising(raise, unstarted()); },
  "for of a generator": raise => { for (var x of raising(raise, unstarted())); },
  "Array.from of a generator": raise => Array.from(raising(raise, unstarted())),
  "construction of a Map": raise => new Map((raise(), [Object.defineProperty([], 0, {get: ran})])),
};

// The operations that may take long: source returns what they work on and start the function at
// the start of which an interruption is requested, unless the operation has none and requests
// it itself.
var pairList = n => range(n).map(i => [i, i]);
var properties = n => Object.fromEntries(range(n).map(i => ["k" + i, i]));
var long = {
  "spread": {source: () => range(7e4), run: (v, raise) => { raise(); return [...v]; }},
  "spread construction": {source: () => range(7e4), run: (v, raise) => { raise(); return new Array(0, ...v); }},
  "for of": {source: () => range(7e4), run: (v, raise) => { raise(); for (var x of v); }},
  "for of pairs": {source: () => pairList(7e4), run: (v, raise) => { raise(); for (var [x, y] of v); }},
  "rest": {source: () => range(7e4), run: (v, raise) => { raise(); var [...x] = v; }},
  "object spread": {source: () => properties(7e4), run: (v, raise) => { raise(); return {...v}; }, prepare: plain},
  "object rest": {source: () => properties(7e4), run: (v, raise) => { raise(); var {k0, ...x} = v; }, prepare: plain},
  "Array.from": {start: () => Array.from, source: () => range(7e4), run: v => Array.from(v)},
  "Set": {start: () => Set, source: () => range(7e4), run: v => new Set(v)},
  "Set of a Set": {start: () => Set, source: () => new Set(range(7e4)), run: v => new Set(v)},
  "Map": {start: () => Map, source: () => pairList(7e4), run: v => new Map(v)},
  "Map of a Map": {start: () => Map, source: () => new Map(pairList(7e4)), run: v => new Map(v)},
  "WeakMap": {start: () => WeakMap, source: () => range(1e4).map(i => [{}, i]), run: v => new WeakMap(v)},
  "WeakSet": {start: () => WeakSet, source: () => range(1e4).map(i => ({})), run: v => new WeakSet(v)},
  "fromEntries": {start: () => Object.fromEntries, source: () => pairList(7e4), run: v => Object.fromEntries(v)},
  "typed array": {start: () => Float64Array, source: () => range(2e5), run: v => new Float64Array(v)},
  "Promise.all": {start: () => Promise.all, source: () => range(1e4), run: v => Promise.all(v)},
  "replace": {start: () => String.prototype.replace, source: () => "ab".repeat(1e5), run: v => v.replace(/a/g, "-")},
  "replace without a match": {start: () => String.prototype.replace, source: () => "ab".repeat(5e5), run: v => v.replace(/a(?=c)/g, "-")},
  "match": {start: () => RegExp.prototype[Symbol.match], source: () => "ab".repeat(1e5), run: v => v.match(/a/g)},
  "search": {start: () => RegExp.prototype[Symbol.search], source: () => "ab".repeat(5e5), run: v => v.search(/a(?=c)/)},
  "test": {start: () => RegExp.prototype.test, source: () => "ab".repeat(5e5), run: v => /a(?=c)/.test(v)},
  "split": {start: () => RegExp.prototype[Symbol.split], source: () => "ab".repeat(1e5), run: v => v.split(/a/)},
  "Object.keys": {start: () => Object.keys, source: () => properties(7e4), run: v => Object.keys(v), prepare: plain},
  "Object.values": {start: () => Object.values, source: () => properties(7e4), run: v => Object.values(v), prepare: plain},
  "Object.assign": {start: () => Object.assign, source: () => properties(7e4), run: v => Object.assign({}, v), prepare: plain},
};
for (let key of ["intersection", "difference", "isSubsetOf", "isDisjointFrom"]) {
  // The last getter that runs requests the interruption.
  long["Set " + key] = {
    source: () => key === "isDisjointFrom" ? [new Set(range(7e4)), new Set(range(7e4).map(i => -1 - i))] : [new Set(range(7e4)), new Set(range(7e4))],
    run: ([s, t], raise) => s[key](Object.defineProperty(t, "keys", {get() { raise(); return Set.prototype.keys; }})),
  };
}
var manyElements = () => Array(2 ** 20 + 1).fill(0);
for (let [key, run, source] of [
  // Copies of few elements take no time.
  ["concat", v => [].concat(v), manyElements],
  ["copyWithin", v => v.copyWithin(1, 0)],
  ["fill", v => v.fill(1)],
  ["flat", v => v.flat()],
  ["includes", v => v.includes(-1, 1)],
  ["indexOf", v => v.indexOf(-1, 1)],
  ["join", v => v.join()],
  ["join", v => v.join(), () => Array(7e4).fill("a")],
  ["lastIndexOf", v => v.lastIndexOf(-1)],
  ["map", v => v.map(Math.abs)],
  ["reverse", v => v.reverse(), () => range(2e5)],
  ["slice", v => v.slice(), manyElements],
  ["sort", v => v.sort()],
  ["sort", v => v.sort(Math.max)],
  ["splice", v => v.splice(0), manyElements],
  ["toReversed", v => v.toReversed()],
  ["toSorted", v => v.toSorted()],
  ["toSpliced", v => v.toSpliced(1, 1)],
  ["unshift", v => v.unshift(1), manyElements],
  ["with", v => v.with(0, 1)],
]) {
  long[key + " " + run + " " + (source || "")] = {start: () => Array.prototype[key], source: source || (() => range(7e4)), run, prepare: plain};
}
