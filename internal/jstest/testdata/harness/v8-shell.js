// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

(function (readFile, stdout, stderr, timer) {
  "use strict";
  const {Error, Uint8Array} = globalThis;
  function getter(proto, name) {
    return Function.call.bind(Object.getOwnPropertyDescriptor(proto, name).get);
  }
  const bufferOf = getter(Object.getPrototypeOf(Uint8Array.prototype), "buffer");
  const descriptionOf = getter(Symbol.prototype, "description");
  const stackOf = getter(Error.prototype, "stack");
  const indexOf = Function.call.bind(String.prototype.indexOf);
  const slice = Function.call.bind(String.prototype.slice);

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
