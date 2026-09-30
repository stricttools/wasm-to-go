// The workload of Test_quickjs: its result() uses only operations whose
// results do not depend on the C library's math functions, so a native
// build of quickjs-ng with the same qb.c gives expected.txt.
function fib(n) {
  return n < 2 ? n : fib(n - 1) + fib(n - 2);
}

class Vec {
  constructor(x, y) { this.x = x; this.y = y; }
  add(o) { return new Vec(this.x + o.x, this.y + o.y); }
}

function result() {
  let h = 0;
  for (let i = 0; i < 20000; i++) h = (h * 31 + (i ^ (i >>> 3))) | 0;
  const counts = new Map();
  for (const w of "the quick brown fox jumps over the lazy dog the end".split(" ")) {
    counts.set(w, (counts.get(w) || 0) + 1);
  }
  let v = new Vec(0, 0);
  for (let i = 0; i < 1000; i++) v = v.add(new Vec(i, -i / 4));
  const f64 = new Float64Array([1.5, -0, NaN, Infinity]);
  const u8 = new Uint8Array(f64.buffer).slice(8, 16);
  return JSON.stringify({
    fib: fib(22),
    h,
    sqrt: Math.sqrt(2),
    third: 1 / 3,
    fixed: (0.1 + 0.2).toFixed(17),
    big: String(2n ** 100n),
    sorted: [5, 3, 9, 1, 7].sort((a, b) => a - b),
    words: [...counts].sort(),
    vec: [v.x, v.y],
    bytes: Array.from(u8),
    re: "a1b22c333".replace(/\d+/g, (d) => d.length),
    date: new Date(0).toISOString(),
    json: JSON.parse('{"a":[1,{"b":null}]}').a[1].b === null,
    str: "Straße".toUpperCase() + "ǅ".toLowerCase(),
  });
}

function step() {}
