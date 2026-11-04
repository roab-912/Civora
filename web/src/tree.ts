import { canonical, domain, sha, type Entry } from "./protocol.js";
const parent = (l: Buffer, r: Buffer): Buffer => sha(Buffer.from([1]), l, r);
const empty: Buffer[] = [];
empty[256] = sha(Buffer.from([2]));
for (let i = 255; i >= 0; i--) empty[i] = parent(empty[i + 1]!, empty[i + 1]!);
export function stateRoot(entries: Entry[]): string {
  const leaves = entries
    .map((e) => {
      const key = Buffer.from(domain("state/key", [e.table, e.key]), "hex");
      return {
        key,
        hash: sha(Buffer.from([0]), key, sha(Buffer.from(canonical(e.value)))),
      };
    })
    .sort((a, b) => Buffer.compare(a.key, b.key));
  for (let i = 1; i < leaves.length; i++)
    if (leaves[i]!.key.equals(leaves[i - 1]!.key))
      throw Error("duplicate state key");
  function walk(lo: number, hi: number, depth: number): Buffer {
    if (lo === hi) return empty[depth]!;
    if (depth === 256) return leaves[lo]!.hash;
    let split = lo;
    while (
      split < hi &&
      !(leaves[split]!.key[Math.floor(depth / 8)]! & (1 << (7 - (depth % 8))))
    )
      split++;
    return parent(walk(lo, split, depth + 1), walk(split, hi, depth + 1));
  }
  return walk(0, leaves.length, 0).toString("hex");
}
