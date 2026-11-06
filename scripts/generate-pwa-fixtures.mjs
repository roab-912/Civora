// Deterministic entropy exists only in this fixture generator. The shipped
// worker always obtains fresh randomness from crypto.getRandomValues.
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { sha, canonical } from "../dist/web/src/protocol.js";
import { verifyCeremony } from "../dist/web/src/electionguard.js";
import { encryptVote } from "../dist/web/src/pwa/encrypt.js";
const check = process.argv.includes("--check"),
  f = JSON.parse(readFileSync("spec/fixtures/v0.4/crypto.json", "utf8"))
    .cases[0],
  e = await verifyCeremony(f.context, f.ceremony_bundle),
  original = crypto.getRandomValues;
let counter = 0;
Object.defineProperty(crypto, "getRandomValues", {
  configurable: true,
  value: (array) => {
    let offset = 0;
    while (offset < array.byteLength) {
      const block = sha(
        Buffer.from(
          "CIVORA/v0.7/PUBLIC_TEST_ONLY/browser-entropy/" + counter++,
        ),
      );
      for (const byte of block) {
        if (offset === array.byteLength) break;
        new Uint8Array(array.buffer, array.byteOffset, array.byteLength)[
          offset++
        ] = byte;
      }
    }
    return array;
  },
});
try {
  const ballots = [],
    secrets = [];
  for (const choice of [0, 1]) {
    const b = await encryptVote(e, choice);
    ballots.push(b.encoded);
    secrets.push({
      choice_TEST_ONLY: String(choice),
      selection_nonce_TEST_ONLY: Buffer.from(b.nonce).toString("base64url"),
    });
    b.nonce.fill(0);
  }
  const files = {
    "browser-crypto.json": {
      label: "PUBLIC_TEST_ONLY_BROWSER_ENCRYPTION",
      context: f.context,
      ceremony_bundle: f.ceremony_bundle,
      ballots,
    },
    "browser-encryption.TEST_ONLY.json": {
      label: "PUBLIC_TEST_ONLY_NEVER_USE_THIS_ENTROPY",
      secrets,
    },
  };
  mkdirSync("spec/fixtures/v0.7", { recursive: true });
  for (const [name, value] of Object.entries(files)) {
    const path = "spec/fixtures/v0.7/" + name,
      raw = canonical(value);
    if (check) {
      if (readFileSync(path, "utf8") !== raw)
        throw Error("Stale browser fixture: " + path);
    } else writeFileSync(path, raw);
  }
} finally {
  Object.defineProperty(crypto, "getRandomValues", {
    configurable: true,
    value: original,
  });
}
