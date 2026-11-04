// Real encrypted development ballots; all private material stays in TEST_ONLY files.
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { Identity } from "@semaphore-protocol/identity";
import { Group } from "@semaphore-protocol/group";
import { generateProof, verifyProof } from "@semaphore-protocol/proof";
import {
  canonical,
  domain,
  sha,
  signTransaction,
  signDigest,
} from "../dist/web/src/protocol.js";
import { binding } from "../dist/web/src/eligibility.js";
import { implementationDigest } from "./electionguard-lock.mjs";
const check = process.argv.includes("--check"),
  newProofs = process.argv.includes("--new-proofs");
if (check && newProofs) throw Error("cannot regenerate proofs in check mode");
const read = (p) => JSON.parse(readFileSync(p, "utf8"));
const write = (path, v) => {
  const raw = canonical(v);
  if (check) {
    if (readFileSync(path, "utf8") !== raw)
      throw Error("stale encrypted fixture: " + path);
  } else writeFileSync(path, raw);
};
const request = read(".tools/electionguard/request.json"),
  keys = read("spec/fixtures/v0.4/actors.TEST_ONLY.json"),
  crypto = read("spec/fixtures/v0.4/crypto.json");
const membershipPath = "spec/fixtures/v0.4/memberships.json";
const old =
  existsSync(membershipPath) && !newProofs
    ? read(membershipPath)
    : { contexts: [] };
const memberships = [],
  sources = [];
for (const r of request.contexts) {
  const actors = keys.contexts.find((c) => c.label === r.label).actors,
    c = crypto.cases.find((c) => c.label === r.label);
  const sorted = actors.toSorted((a, b) =>
      a.account_id < b.account_id ? -1 : 1,
    ),
    group = new Group(sorted.map((a) => a.commitment));
  const proofs = [];
  for (const [i, ballot] of c.ballots.entries()) {
    const expected = await binding(
      r.network_id,
      r.poll_id,
      Buffer.from(ballot, "base64url"),
    );
    let p = old.contexts.find((c) => c.label === r.label)?.proofs[i];
    if (!p) {
      if (check) throw Error("missing frozen membership proof");
      const sdk = await generateProof(
        Identity.import(actors[i].identity_export_TEST_ONLY),
        group,
        expected.message,
        expected.scope,
        16,
        {
          wasm: ".tools/semaphore/16/semaphore.wasm",
          zkey: ".tools/semaphore/16/semaphore.zkey",
        },
      );
      p = { ...sdk, merkleTreeDepth: "16" };
    }
    if (
      p.message !== expected.message ||
      p.scope !== expected.scope ||
      p.merkleTreeRoot !== String(group.root) ||
      !(await verifyProof({ ...p, merkleTreeDepth: 16 }))
    )
      throw Error("frozen membership/context failed");
    proofs.push(p);
  }
  memberships.push({ label: r.label, proofs });
  const genesis = structuredClone(
    read("spec/fixtures/v0.3/accounts.json").genesis,
  );
  genesis.network_id = r.network_id;
  genesis.chain_id = r.chain_id;
  // The consensus and unimplemented prepared modules remain inactive.
  const implementation = implementationDigest();
  genesis.registry.modules.push({
    id: "eg-2.0-civic-1",
    status: "active",
    operations: ["ballot.prove", "tally.prove"],
    parameters: { format: "eg-civic-compact-1" },
    implementation_sha256: implementation,
    artifacts: [
      sha(
        readFileSync("spec/crypto/electionguard-v2.parameters.json"),
      ).toString("hex"),
      sha(
        readFileSync("spec/crypto/electionguard-v2.manifest-template.json"),
      ).toString("hex"),
    ],
    dependencies: [genesis.eligibility.suite_id],
    retirement_policy: "never_reactivate_revoked",
  });
  const governance = structuredClone(
    read("spec/fixtures/v0.2/governance.json").genesis.governance,
  );
  governance.crypto_profile = "electionguard_v0_4";
  governance.ballot_suite = "eg-2.0-civic-1";
  governance.tally_suite = "eg-2.0-civic-1";
  governance.eligibility_suite = genesis.eligibility.suite_id;
  governance.guardians = actors.slice(0, 5).map((a) => ({
    account_id: a.account_id,
    signing_pk: a.signing.publicKey,
  }));
  genesis.governance = governance;
  const signed = (i, type, nonce, payload) =>
    signTransaction(
      {
        v: "1",
        network_id: r.network_id,
        type,
        actor_id: actors[i].account_id,
        nonce,
        payload,
      },
      actors[i].signing.seed,
    );
  const proof = (type, payload) => ({
    body: {
      v: "1",
      network_id: r.network_id,
      type,
      actor_id: null,
      nonce: null,
      payload,
    },
    auth: { scheme: "proof" },
  });
  const block = (height, time_ms, transactions = []) => ({
    height: String(height),
    time_ms: String(time_ms),
    transactions,
  });
  const ack = domain("poll/ceremony", [
    r.network_id,
    r.poll_id,
    sha(Buffer.from(c.ceremony_bundle, "base64url")).toString("hex"),
  ]);
  const guardian_acks = r.guardian_ids.map((id) => {
    const a = actors.find((a) => a.account_id === id);
    return { account_id: id, signature: signDigest(ack, a.signing.seed) };
  });
  const aggregate_digest = domain("poll/aggregate", [
    r.network_id,
    r.poll_id,
    "7",
    c.count,
    c.aggregate,
  ]);
  const blocks = [
    block(
      1,
      1000,
      actors.map((a, i) =>
        signed(i, "account.register", "0", {
          signing_pk: a.signing.publicKey,
          recovery_pk: a.recovery.publicKey,
          commitment: a.commitment,
        }),
      ),
    ),
    block(2, 1001, [
      r.proposal_transaction,
      signed(0, "proposal.endorse", "1", { proposal_id: r.proposal_id }),
      signed(1, "proposal.endorse", "1", { proposal_id: r.proposal_id }),
    ]),
    block(3, 1002),
    block(4, 1003, [
      proof("poll.configure", {
        poll_id: r.poll_id,
        ceremony_bundle: c.ceremony_bundle,
        guardian_acks,
      }),
    ]),
    block(5, 3601003),
    block(
      6,
      3601004,
      c.ballots.map((ballot, i) =>
        proof("vote.cast", {
          poll_id: r.poll_id,
          encrypted_ballot: ballot,
          membership: proofs[i],
        }),
      ),
    ),
    block(7, 7201003),
    block(8, 7201004, [
      proof("poll.tally", {
        poll_id: r.poll_id,
        yes: c.yes,
        no: c.no,
        aggregate_digest,
        decryption_record: c.tally_bundle,
      }),
    ]),
    block(9, 10801004),
  ];
  sources.push({
    label: r.label,
    name: `TEST_ONLY real ElectionGuard 2.0 ${r.label}`,
    genesis,
    blocks,
  });
}
write(membershipPath, {
  label: "PUBLIC_SEMAPHORE_PROOFS_BOUND_TO_REAL_ENCRYPTED_BALLOTS",
  contexts: memberships,
});
write("spec/fixtures/v0.4/replay-source.json", { cases: sources });
if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
