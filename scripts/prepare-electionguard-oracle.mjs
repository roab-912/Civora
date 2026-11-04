// Deterministic TEST_ONLY SDK request. No simulator is linked into a node.
import { readFileSync, mkdirSync, writeFileSync, existsSync } from "node:fs";
import { Identity } from "@semaphore-protocol/identity";
import {
  canonical,
  chainID,
  domain,
  sha,
  testKey,
  signTransaction,
} from "../dist/web/src/protocol.js";
const outputs = [];
const keys = [];
for (const [label, votes] of [
  ["mixed", [1, 0, 1, 0, 1]],
  ["all-no", [0, 0, 0]],
]) {
  const network = domain("development/electionguard", label);
  const actors = Array.from({ length: 6 }, (_, i) => {
    const signing = testKey(
      sha(Buffer.from(`CIVORA TEST_ONLY v0.4 ${label} signing ${i}`)).toString(
        "hex",
      ),
    );
    const recovery = testKey(
      sha(Buffer.from(`CIVORA TEST_ONLY v0.4 ${label} recovery ${i}`)).toString(
        "hex",
      ),
    );
    const identity = new Identity(
      `CIVORA TEST_ONLY v0.4 ${label} Semaphore ${i}`,
    );
    return {
      signing,
      recovery,
      account_id: domain("account", [network, signing.publicKey]),
      commitment: identity.commitment.toString(),
      identity_export_TEST_ONLY: identity.export(),
    };
  });
  const old = JSON.parse(
    readFileSync("spec/fixtures/v0.2/governance.json", "utf8"),
  );
  const payload = structuredClone(
    old.blocks
      .flatMap((b) => b.transactions)
      .find((t) => t.body.type === "proposal.submit").body.payload,
  );
  payload.rationale = "TEST_ONLY ElectionGuard " + label;
  const tx = signTransaction(
    {
      v: "1",
      network_id: network,
      type: "proposal.submit",
      actor_id: actors[5].account_id,
      nonce: "1",
      payload,
    },
    actors[5].signing.seed,
  );
  const proposal = domain("tx", tx.body);
  const poll = domain("poll", [network, proposal]);
  outputs.push({
    label,
    network_id: network,
    chain_id: chainID(network),
    poll_id: poll,
    proposal_id: proposal,
    proposal_transaction: tx,
    guardian_ids: actors
      .slice(0, 5)
      .map((a) => a.account_id)
      .sort(),
    votes_TEST_ONLY: votes.map(String),
  });
  keys.push({ label, actors });
}
mkdirSync(".tools/electionguard", { recursive: true });
mkdirSync("spec/fixtures/v0.4", { recursive: true });
for (const [path, value] of [
  [".tools/electionguard/request.json", { contexts: outputs }],
  [
    "spec/fixtures/v0.4/actors.TEST_ONLY.json",
    {
      label: "PUBLIC_TEST_ONLY_PRIVATE_KEYS_NEVER_USE_IN_PRODUCTION",
      contexts: keys,
    },
  ],
]) {
  const raw = canonical(value);
  if (existsSync(path) && readFileSync(path, "utf8") === raw) continue;
  if (process.argv.includes("--check") && !path.startsWith(".tools/")) {
    if (readFileSync(path, "utf8") !== raw)
      throw Error("stale TEST_ONLY actors");
  } else writeFileSync(path, raw);
}
