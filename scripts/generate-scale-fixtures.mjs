import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { Identity } from "@semaphore-protocol/identity";
import { Group } from "@semaphore-protocol/group";
import { generateProof, verifyProof } from "@semaphore-protocol/proof";
import { canonical, domain, sha } from "../dist/web/src/protocol.js";
import { decodeBundle } from "../dist/web/src/electionguard-bundle.js";
import { verifyCeremony } from "../dist/web/src/electionguard.js";
import {
  createIndependence,
  lane,
  scaleBinding,
  verifyScale,
} from "../dist/web/src/electionguard-scale.js";
const read = (p) => JSON.parse(readFileSync(p, "utf8")),
  path = "spec/fixtures/v0.4/scale.json",
  check = process.argv.includes("--check");
const old =
  existsSync(path) && !process.argv.includes("--new-proofs")
    ? read(path)
    : null;
if (check && !old) throw Error("missing frozen scale proofs");
const f = read("spec/fixtures/v0.4/crypto.json").cases[0],
  oracle = read("spec/fixtures/v0.4/oracle.TEST_ONLY.json").contexts[0],
  actors = read("spec/fixtures/v0.4/actors.TEST_ONLY.json").contexts[0].actors;
const group = new Group(
  actors
    .toSorted((a, b) => (a.account_id < b.account_id ? -1 : 1))
    .map((a) => a.commitment),
);
const context = {
  network_id: f.context.network_id,
  poll_id: f.context.poll_id,
  protocol_epoch: "1",
  group_root: String(group.root),
  circuit_depth: "26",
  partition_count: "32",
  checkpoint_id: domain("test/checkpoint", [
    f.context.network_id,
    f.context.poll_id,
  ]),
};
const election = await verifyCeremony(f.context, f.ceremony_bundle),
  ballot = f.ballots[0],
  bytes = decodeBundle(ballot, "ballot").files.get("ballot.json"),
  digest = sha(bytes).toString("hex"),
  alpha = JSON.parse(new TextDecoder().decode(bytes)).contests[0].selections[0]
    .encrypted_vote.alpha;
async function prove(identity, message, scope) {
  const proof = await generateProof(identity, group, message, scope, 26, {
    wasm: ".tools/semaphore/26/semaphore.wasm",
    zkey: ".tools/semaphore/26/semaphore.zkey",
  });
  return { ...proof, merkleTreeDepth: "26" };
}
const scope = (await scaleBinding(context, "00".repeat(32), "00".repeat(32)))
  .scope;
const validIdentity = Identity.import(actors[0].identity_export_TEST_ONLY),
  attacker = Identity.import(actors[1].identity_export_TEST_ONLY);
const baseNullifier =
  old?.valid.membership.nullifier ??
  (await prove(validIdentity, "0", scope)).nullifier;
const nonce = Buffer.from(
    oracle.ballots[0].selection_nonce_TEST_ONLY,
    "base64url",
  ),
  random = sha(
    Buffer.from("CIVORA TEST_ONLY RFC8235 nonce " + context.poll_id),
  );
const independence = await createIndependence(
  context,
  baseNullifier,
  digest,
  alpha,
  nonce,
  random,
);
if (nonce.some(Boolean) || random.some(Boolean))
  throw Error("nonce inputs were not wiped");
const binding = await scaleBinding(
  context,
  digest,
  sha(Buffer.from(canonical(independence))).toString("hex"),
);
let membership =
  old?.valid.membership ??
  (await prove(validIdentity, binding.message, binding.scope));
if (
  membership.nullifier !== baseNullifier ||
  membership.scope !== binding.scope ||
  membership.message !== binding.message ||
  !(await verifyProof({ ...membership, merkleTreeDepth: 26 }))
)
  throw Error("scale binding diverged");
const valid = {
  network_id: context.network_id,
  poll_id: context.poll_id,
  protocol_epoch: "1",
  lane_id: await lane(context.network_id, context.poll_id, baseNullifier, "32"),
  checkpoint_id: context.checkpoint_id,
  encrypted_ballot: ballot,
  independence,
  membership,
};
await verifyScale(election, context, valid);
// The copied ballot has a REAL membership proof from a second identity, tied
// to these exact bytes. Only knowledge of the original encryption nonce is absent.
const attackerMembership =
  old?.copy_attack.membership ??
  (await prove(attacker, binding.message, binding.scope));
if (
  !(await verifyProof({ ...attackerMembership, merkleTreeDepth: 26 })) ||
  attackerMembership.message !== binding.message ||
  attackerMembership.scope !== binding.scope ||
  attackerMembership.nullifier === baseNullifier
)
  throw Error("copy attack fixture lacks real second-identity proof");
const copy_attack = {
  ...valid,
  lane_id: await lane(
    context.network_id,
    context.poll_id,
    attackerMembership.nullifier,
    "32",
  ),
  membership: attackerMembership,
};
await verifyScale(election, context, copy_attack).then(
  () => {
    throw Error("copied ballot accepted");
  },
  () => {},
);
const data = {
  label:
    "DEVELOPMENT_VERIFICATION_ONLY_CHECKPOINT_CONTEXT_SUPPLIED_BY_FIXTURE_NOT_BFT",
  context,
  valid,
  copy_attack,
};
const out = canonical(data);
if (check) {
  if (readFileSync(path, "utf8") !== out) throw Error("stale scale fixture");
} else writeFileSync(path, out);
if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
