// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// The operands that the shortcuts of built-in functions are tested with, and the functions that
// call a built-in with them and describe what happened. Every probe creates its operands anew.

var log = [];
var proxies = new WeakSet;
var running;

// hook returns a function that logs how it was called, and then returns v.
function hook(name, v) {
  function h() {
    var caller = h.caller;
    log.push(name + " from " + (caller === null ? "native" : caller === running ? "site" : "other") + trace(new Error));
    return v;
  }
  return h;
}

function trace(e) {
  return e.stack.split("\n    at ").slice(1).map(function (s) { return "\n  " + s; }).join("");
}

function getter(o, key, v) {
  return Object.defineProperty(o, key, {get: hook("get " + String(key), v), configurable: true});
}

function setter(o, key) {
  return Object.defineProperty(o, key, {set: hook("set " + String(key)), configurable: true});
}

function proxy(target) {
  var handler = {};
  Reflect.ownKeys(Reflect).forEach(function (name) {
    if (typeof Reflect[name] === "function") {
      handler[name] = function trap() {
        var caller = trap.caller;
        log.push(name + " from " + (caller === null ? "native" : caller === running ? "site" : "other") + trace(new Error));
        return Reflect[name].apply(Reflect, arguments);
      };
    }
  });
  var p = new Proxy(target, handler);
  proxies.add(p);
  return p;
}

function revoked(target) {
  var r = Proxy.revocable(target, {});
  r.revoke();
  proxies.add(r.proxy);
  return r.proxy;
}

function holes() {
  var a = [1, , 3];
  Object.setPrototypeOf(a, getter(Object.create(Array.prototype), 1, 2));
  return a;
}

function range(n) {
  return Array.from({length: n}, function (_, i) { return i; });
}

function subarray(a) {
  return Object.setPrototypeOf(a, getter(setter(Object.create(Array.prototype), 1), 0, "inherited"));
}

var symbol = Symbol("s");
// shared is an object that operands of the same probe have in common.
var shared;

// The operands that take part in every combination come first.
var operands = {
  "undefined": () => undefined,
  "one": () => 1,
  "-1.5": () => -1.5,
  "zero": () => 0,
  "-0": () => -0,
  "NaN": () => NaN,
  "two units": () => "ab",
  "converting object": () => ({toString: hook("toString", "1"), valueOf: hook("valueOf", 1)}),
  "symbol": () => symbol,
  "shared object": () => shared,
  "bigint": () => 10n,

  "null": () => null,
  "false": () => false,
  "0.5": () => 0.5,
  "-0.4": () => -0.4,
  "two": () => 2,
  "Infinity": () => Infinity,
  "-Infinity": () => -Infinity,
  "2**31": () => 2 ** 31,
  "2**53": () => 2 ** 53,
  "-(2**63)": () => -(2 ** 63),
  "2**63": () => 2 ** 63,
  "1e300": () => 1e300,
  "empty string": () => "",
  "numeric string": () => "1",
  "string": () => "a shared string of some length",
  "string of other text": () => "é𝌆" + "z".repeat(20) + "𝌆",
  "low surrogate": () => "\udf06",
  "key": () => "length",
  "large bigint": () => 1n << 70n,
  "object": () => ({a: 1, 1: 2}),
  "object with getters": () => getter(getter({}, "length", 2), 1, "v"),
  "array": () => [1, "ab", shared, NaN, -0, 10n],
  "empty array": () => [],
  "array of no extensions": () => Object.preventExtensions([1, 2]),
  "array of frozen length": () => Object.defineProperty([1, 2], "length", {writable: false}),
  "frozen array": () => Object.freeze([1, 2]),
  "array with holes": holes,
  "array with an accessor": () => setter(getter([0, 0], 1, "v"), 2),
  "array with an inherited accessor": () => subarray([]),
  "array of the largest length": () => { var a = []; a.length = 2 ** 32 - 1; return a; },
  "sparse array": () => { var a = []; a[1e6] = 1; return a; },
  "array-like with setters": () => setter(setter({length: 1}, "length"), 1),
  "arguments": () => (function () { return arguments; })(1, 2),
  "function": () => function f() {},
  "typed array": () => new Uint8Array([1, 2]),
  "String object": () => new String("ab"),
  "String object with a conversion": () => getter(new String("ab"), "toString", () => "ab"),
  "Number object": () => new Number(1),
  "Map": () => new Map([[1, "one"], ["ab", "two"], [shared, "three"], [NaN, "n"], [0, "z"], [10n, "b"]]),
  "large Map": () => new Map(range(70).map(i => [i, "v" + i])),
  "Set": () => new Set([1, "ab", shared, NaN, 0, 10n]),
  "large Set": () => new Set(range(70)),
  "WeakMap": () => new WeakMap,
  "Date": () => new Date(0),
  "RegExp": () => /a/g,
  "object with toPrimitive": () => ({[Symbol.toPrimitive]: hook("toPrimitive", 1)}),
  "object with toPrimitive to a symbol": () => ({[Symbol.toPrimitive]: hook("toPrimitive", symbol)}),
  "object with a throwing conversion": () => ({toString() { throw new EvalError; }, valueOf() { throw new EvalError; }}),
  "proxy of an object": () => proxy({a: 1}),
  "proxy of an array": () => proxy([1, 2]),
  "proxy of a Map": () => proxy(new Map),
  "revoked proxy": () => revoked([]),
};

// The operands that a built-in function may take long to work through.
var largeOperands = {
  "string": () => "a".repeat(1.2e6),
  "string of other text": () => "é".repeat(1.2e6),
  "array": () => range(1.5e5),
  "typed array": () => new Uint8Array(1.2e6),
  "bigint": () => 1n << 1000000n,
  "Map": () => new Map(range(1.5e5).map(i => [i, i])),
  "Set": () => new Set(range(1.5e5)),
  "absent string": () => "b",
  "absent long string": () => "a".repeat(40) + "b",
  "low surrogate": () => "\udf06",
  "zero": () => 0,
  "1e9": () => 1e9,
  "undefined": () => undefined,
};

var large = {};
for (var key in largeOperands) large[key] = largeOperands[key]();

function iterator(proto) {
  return Object.assign(Object.create(Iterator.prototype), proto);
}

var pairs = () => [[1, 2], [3, 4]];

// The iterators that are stepped with every next method.
var iterators = {
  "array values": () => [1, 2, 3].values(),
  "array keys": () => [1, 2, 3].keys(),
  "array entries": () => ["a", "b"].entries(),
  "array of pairs": () => pairs().values(),
  "values of an array with a hole": () => holes().values(),
  "entries of an array with a hole": () => holes().entries(),
  "values of an array with a throwing getter": () => Object.defineProperty([1, 2], 1, {get() { throw new EvalError; }}).values(),
  "entries of an array-like": () => Array.prototype.entries.call(getter(getter({}, "length", 1), 0, "v")),
  "values of a proxy": () => Array.prototype.values.call(proxy([1, 2])),
  "values of a typed array": () => new Uint8Array([1, 2]).values(),
  "entries of a typed array": () => new Uint8Array([1, 2]).entries(),
  "values of a detached typed array": () => { var a = new Uint8Array(2), it = a.values(); a.buffer.transfer(); return it; },
  "Map entries": () => new Map(pairs()).entries(),
  "Map keys": () => new Map(pairs()).keys(),
  "Map values": () => new Map(pairs()).values(),
  "Set entries": () => new Set([1, 2]).entries(),
  "Set values": () => new Set(pairs()).values(),
  "finished Set values": () => { var it = new Set([1]).values(); it.next(); it.next(); return it; },
  "string": () => "a𝌆b"[Symbol.iterator](),
  "matchAll": () => "aab".matchAll(/a/g),
  "matchAll with an own exec": () => {
    var it = "aab".matchAll(Object.assign(/a/g, {constructor: {[Symbol.species]: function (r, f) {
      return getter(new RegExp(r, f), "exec", hook("exec", null));
    }}}));
    return it;
  },
  "map helper": () => [1, 2].values().map(hook("mapper", [5, 6])),
  "filter helper": () => [1, 2].values().filter(hook("predicate", true)),
  "flatMap helper": () => [1].values().flatMap(hook("mapper", [7, 8])),
  "take helper": () => [1, 2].values().take(1),
  "drop helper": () => getter([1, 2], 0, 1).values().drop(1),
  "helper of an iterator with a throwing next": () => iterator({next() { throw new EvalError; }}).take(1),
  "concat": () => Iterator.concat([1], new Set([2])),
  "zip": () => Iterator.zip([[1, 2], new Map(pairs())]),
  "object": () => ({}),
};

// The iterators with steps that may take long.
var largeIterators = {
  "drop helper": () => (function* () { for (;;) yield; })().drop(1e6),
  "matchAll": () => "a".repeat(20000).matchAll(/a*b/g),
  "array values": () => range(10).values(),
  "values of an array with a hole": () => [,].values(),
  "string": () => "ab"[Symbol.iterator](),
  "Map entries": () => new Map(pairs()).entries(),
  "Set entries": () => new Set([1]).entries(),
};

function quote(s) {
  return JSON.stringify(s.length > 80 ? s.length + " units, " + s.slice(0, 40) + s.slice(-20) : s);
}

// name describes the value v, naming the operands by identity.
function name(v, named) {
  switch (typeof v) {
    case "string": return quote(v);
    case "number": return Object.is(v, -0) ? "-0" : String(v);
    case "bigint": return v + "n";
    case "object":
    case "function":
      if (v === null) return "null";
      for (var i = 0; i < named.length; i++) {
        if (named[i] === v) return "#" + i;
      }
      if (v === shared) return "shared";
      return v instanceof Error ? v.constructor.name + trace(v) : typeof v;
  }
  return String(v);
}

// contents describes the state of the operand or result v, without running hooks.
function contents(v, named) {
  if (Object(v) !== v || proxies.has(v)) return "";
  var out = [];
  for (var key of Reflect.ownKeys(v).slice(0, 100)) {
    var d = Object.getOwnPropertyDescriptor(v, key);
    out.push(String(key) + "=" + ("value" in d ? name(d.value, named) : "accessor") + (d.writable ? "w" : "") + (d.enumerable ? "e" : "") + (d.configurable ? "c" : ""));
  }
  if (v instanceof Map) v.forEach((x, k) => out.push(name(k, named) + "=>" + name(x, named)));
  if (v instanceof Set) v.forEach(k => out.push(name(k, named)));
  return "{" + out.join() + "}";
}

// probe calls f at site with the operands keys, the first of which is the this value. It returns
// the number of hooks that ran and a description of what happened.
function probe(site, f, keys) {
  shared = {name: "shared"};
  var named = keys.map(function (k) { return operands[k](); });
  log = [];
  running = site;
  var out;
  try {
    var r = site(f, ...named);
    out = "returned " + name(r, named) + contents(r, named);
  } catch (e) {
    out = "threw " + name(e, named);
  }
  running = null;
  var hooks = log;
  return hooks.length + " hooks: " + out + "\n" + hooks.join("\n") + "\n" + named.map(function (v) { return contents(v, named); }).join("\n");
}

// probeIterator steps an iterator with next at site.
function probeIterator(site, next, key, generic) {
  var it = iterators[key]();
  log = [];
  running = site;
  var out;
  try {
    out = "returned " + JSON.stringify(site(stepped(it, next, generic)));
  } catch (e) {
    out = "threw " + name(e, []);
  }
  running = null;
  return log.length + " hooks: " + out + "\n" + log.join("\n");
}

// stepped returns an iterable whose iterator steps it with next. The iterator is it itself or,
// when generic is set, an object whose next method is a bound function, which shows no frame and
// makes the generic call.
function stepped(it, next, generic) {
  if (!generic) {
    it.next = next;
    it[Symbol.iterator] = function () { return this; };
    return it;
  }
  var r = it.return;
  return {
    __proto__: null,
    next: next.bind(it),
    return: r && r.bind(it),
    [Symbol.iterator]() { return this; },
  };
}

function stepSite(it) {
  var out = [];
  for (var v of it) if (out.push(v) === 6) break;
  return out;
}

function pairSite(it) {
  var out = [];
  for (var [a, b] of it) if (out.push(a, b) >= 6) break;
  return out;
}
