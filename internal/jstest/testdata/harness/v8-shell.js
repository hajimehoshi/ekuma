// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

(function (installShell) {
  return function (shell) {
    installShell(shell);
    // d8 creates fresh prototypes per realm and shares the node receiver brand.
    const domNodes = shell.domNodes ?? new WeakSet();
    const addNode = WeakSet.prototype.add.bind(domNodes);
    const isNode = WeakSet.prototype.has.bind(domNodes);
    const {TypeError} = globalThis;
    // Template constructors use non-strict receiver conversion on ordinary calls.
    function EventTarget() { return this; }
    function nodeConstructor(parent, name) {
      function ctor() {
        if (new.target) addNode(this);
        return this;
      }
      Object.defineProperty(ctor, "name", {value: name});
      Object.setPrototypeOf(ctor.prototype, parent.prototype);
      return ctor;
    }
    const Node = nodeConstructor(EventTarget, "");
    Object.defineProperty(Node.prototype, "nodeType", {
      get() {
        if (!isNode(this)) throw new TypeError("Illegal invocation");
        return 1;
      },
      enumerable: true, configurable: true,
    });
    const Element = nodeConstructor(Node, "");
    const HTMLElement = nodeConstructor(Element, "");
    d8.dom = {EventTarget, Div: nodeConstructor(HTMLElement, "Div")};
    // Scripts can attach test state to this realm's d8.test object.
    d8.test = {};
    return domNodes;
  };
})(function (shell) {
  "use strict";
  const {readFile, stdout, stderr, timer, exit, canAccessReceiver} = shell;
  const {now: timestamp, recordMark, lookupMark} = shell;
  const {Error, TypeError, Uint8Array} = globalThis;
  const {defineProperty} = Object;
  function getter(proto, name) {
    return Function.call.bind(Object.getOwnPropertyDescriptor(proto, name).get);
  }
  const bufferOf = getter(Object.getPrototypeOf(Uint8Array.prototype), "buffer");
  const descriptionOf = getter(Symbol.prototype, "description");
  const stackOf = getter(Error.prototype, "stack");
  const indexOf = Function.call.bind(String.prototype.indexOf);
  const slice = Function.call.bind(String.prototype.slice);

  globalThis.version = function version() {
    if (!canAccessReceiver(this)) throw new TypeError("no access");
    // V8_VERSION_STRING at the revision in v8-revision.txt.
    return "16.0.0 (candidate)";
  };

  function readOnly(object, key, value) {
    defineProperty(object, key, {__proto__: null, value, enumerable: true, configurable: true});
  }
  // d8 exposes os.system only with --enable-os-system.
  const os = {};
  readOnly(os, "name", shell.osName);
  readOnly(os, "d8Path", shell.d8Path);
  globalThis.os = os;

  function entry(entryType, name, startTime, duration) {
    const result = {};
    readOnly(result, "entryType", entryType);
    readOnly(result, "name", name);
    readOnly(result, "startTime", startTime);
    readOnly(result, "duration", duration);
    return result;
  }
  globalThis.performance = {
    now: function now() { return timestamp(); },
    mark: function mark(name = undefined) {
      if (typeof name !== "string") throw new Error("Invalid 'name' argument");
      return entry("mark", name, recordMark(name), 0);
    },
    measure: function measure(name = undefined, startMark, endMark) {
      if (typeof name !== "string") throw new Error("Invalid 'name' argument");
      let start = 0;
      let end = timestamp();
      if (typeof startMark === "string") {
        start = lookupMark(startMark);
        if (arguments.length === 3) {
          if (typeof endMark !== "string") throw new Error("Expect string as end mark.");
          end = lookupMark(endMark);
        }
      } else if (startMark !== undefined) {
        if (startMark === null || (typeof startMark !== "object" && typeof startMark !== "function")) {
          throw new Error("Invalid 'startMark' argument: Not an Object");
        }
        if (arguments.length > 2) throw new Error("Too many arguments");
        // Legacy d8 accepts a mark-like object with a numeric startTime.
        start = startMark.startTime;
        if (typeof start !== "number") {
          throw new Error("Invalid 'startMark' argument: No numeric 'startTime' field");
        }
      }
      return entry("measure", name, start, end - start);
    },
  };

  for (const target of [globalThis, d8]) {
    target.quit = function quit(code = 0) {
      try {
        code |= 0;
      } catch {
        code = 0;
      }
      exit(code);
    };
  }

  globalThis.readbuffer = function readbuffer(name = undefined) {
    const view = readFile(`${name}`, Uint8Array);
    if (typeof view === "string") throw new Error(view);
    return bufferOf(view);
  };

  function write(out, args) {
    for (let i = 0; i < args.length; i++) {
      if (i) out(" ");
      const arg = args[i];
      out(`${typeof arg === "symbol" ? descriptionOf(arg) : arg}`);
    }
  }
  globalThis.printErr = function printErr(...args) {
    try {
      write(stderr, args);
    } finally {
      stderr("\n");
    }
  };

  function line(out, prefix, args) {
    if (prefix) out(prefix);
    write(out, args);
    out("\n");
  }
  function label(args) {
    return args.length ? `${args[0]}` : "default";
  }
  const console = {
    log(...args) { line(stdout, "", args); },
    info(...args) { line(stdout, "console.info: ", args); },
    debug(...args) { line(stdout, "console.debug: ", args); },
    warn(...args) { line(stdout, "console.warn: ", args); },
    error(...args) { line(stderr, "console.error: ", args); },
    assert(...args) {
      if (args.length && args[0]) return;
      line(stdout, "console.assert: ", args);
      throw new Error("console.assert failed");
    },
    time(...args) { timer("time", label(args)); },
    timeLog(...args) { timer("timeLog", label(args)); },
    timeEnd(...args) { timer("timeEnd", label(args)); },
    timeStamp(...args) { timer("timeStamp", label(args)); },
    trace() {
      const stack = stackOf(new Error());
      // Omit the error heading and the frame for this shell method.
      const headingEnd = indexOf(stack, "\n");
      const frameEnd = indexOf(stack, "\n", headingEnd + 1);
      stderr((frameEnd < 0 ? "" : slice(stack, frameEnd + 1)) + "\n");
    },
    // TODO: Collect a CPU profile when the harness supports profiling.
    profile() {},
  };
  Object.defineProperty(console, Symbol.toStringTag, {value: "console", configurable: true});
  Object.defineProperty(globalThis, "console", {value: console, writable: true, configurable: true});
})
