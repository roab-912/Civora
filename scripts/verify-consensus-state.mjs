import { readFileSync } from "node:fs";
import { verifyState } from "../dist/web/src/consensus.js";
const [trustPath, proofPath] = process.argv.slice(2);
if (!trustPath || !proofPath)
  throw Error("usage: verify-consensus-state trust.json proof.json");
const trust = JSON.parse(readFileSync(trustPath, "utf8")),
  proof = JSON.parse(readFileSync(proofPath, "utf8"));
const h = verifyState(trust, proof);
console.log(
  "Independent TypeScript native state proof verified after height",
  proof.height,
  "by header",
  h.height,
);
