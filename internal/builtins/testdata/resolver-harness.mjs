// node resolver-harness.mjs <resolver.js> <cases.json>
// jDt, WDt and their regexes are copied verbatim from Claude Code 2.1.278.
import { isAbsolute, normalize, resolve } from "path";
import { readFileSync } from "fs";

const qTr = /(^|[\\/])proc[\\/](self|thread-self|\d+)([\\/]|$)/i;
const KTr = /(^|[\\/])dev[\\/](fd|stdin|stdout|stderr)([\\/]|$)/i;

function jDt(e) {
  let n = e.replace(/[\\/]\.(?=[\\/]|$)/g, "");
  return (n = n.replace(/[\\/]{2,}/g, "/")), qTr.test(n) || KTr.test(n);
}

const Dn = () => false;

function WDt(e) {
  if (/^[\\/]System[\\/]Volumes[\\/]/i.test(e)) return !0;
  if (/^[\\/]Volumes[\\/]/i.test(e)) return !0;
  if (/(^|[\\/])proc[\\/]cygdrive([\\/]|$)/i.test(e)) return !0;
  if (/^[\\/]\.\.[\\/]/.test(e)) return !0;
  if (Dn(e)) return !0;
  if (e.includes("~")) return !0;
  return !1;
}

// Stubs: a case whose verdict turns on Di, nl or Dn proves nothing here.
const Di = () => false;
const nl = () => false;

const lk = (e) => e.search(/[*?[]/);
const WTr = isAbsolute;
const Ze = (e, n) => (isAbsolute(e) ? normalize(e) : resolve(n, e));

const src = readFileSync(process.argv[2], "utf8");
const resolver = eval(`${src}\n;r5`);

const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const out = cases.map(([word, cwd, absolute, glob]) => {
  const got = resolver(word, cwd, absolute, glob);
  if (got === null) return { refused: true };
  if (typeof got === "string") return { refused: false, path: got };
  throw new Error(`resolver returned ${typeof got}: ${String(got)}`);
});
process.stdout.write(JSON.stringify(out));
