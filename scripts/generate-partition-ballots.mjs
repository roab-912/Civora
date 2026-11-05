import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { Identity } from "@semaphore-protocol/identity";
import { Group } from "@semaphore-protocol/group";
import { generateProof, verifyProof } from "@semaphore-protocol/proof";
import { canonical, sha } from "../dist/web/src/protocol.js";
import { decodeBundle } from "../dist/web/src/electionguard-bundle.js";
import { verifyCeremony } from "../dist/web/src/electionguard.js";
import {
  createIndependence,
  lane,
  scaleBinding,
  verifyScale,
} from "../dist/web/src/electionguard-scale.js";
const read = (p) => JSON.parse(readFileSync(p, "utf8")),
  path = "spec/fixtures/v0.5/ballots.json",
  check = process.argv.includes("--check"),
  old =
    existsSync(path) && !process.argv.includes("--new-proofs")
      ? read(path)
      : null;
if (check && !old) throw Error("missing frozen v0.5 proofs");
const f = read("spec/fixtures/v0.4/crypto.json").cases[0],
  actors = read("spec/fixtures/v0.4/actors.TEST_ONLY.json").contexts[0].actors,
  oracle = read("spec/fixtures/v0.4/oracle.TEST_ONLY.json").contexts[0],
  scale = read("spec/fixtures/v0.4/scale.json"),
  context = { ...scale.context, partition_count: "8" },
  group = new Group(
    actors
      .toSorted((a, b) => (a.account_id < b.account_id ? -1 : 1))
      .map((a) => a.commitment),
  ),
  election = await verifyCeremony(f.context, f.ceremony_bundle);
async function prove(identity, message, scope) {
  const p = await generateProof(identity, group, message, scope, 26, {
    wasm: ".tools/semaphore/26/semaphore.wasm",
    zkey: ".tools/semaphore/26/semaphore.zkey",
  });
  return { ...p, merkleTreeDepth: "26" };
}
const ballots = [];
for (let i = 0; i < f.ballots.length; i++) {
  const identity = Identity.import(actors[i].identity_export_TEST_ONLY),
    encrypted = f.ballots[i],
    raw = decodeBundle(encrypted, "ballot").files.get("ballot.json"),
    digest = sha(raw).toString("hex"),
    alpha = JSON.parse(new TextDecoder().decode(raw)).contests[0].selections[0]
      .encrypted_vote.alpha,
    scope = (await scaleBinding(context, "00".repeat(32), "00".repeat(32)))
      .scope;
  const previous = old?.ballots[i] ?? (i === 0 ? scale.valid : null),
    nullifier =
      previous?.membership.nullifier ??
      (await prove(identity, "0", scope)).nullifier;
  const independence = await createIndependence(
      context,
      nullifier,
      digest,
      alpha,
      Buffer.from(oracle.ballots[i].selection_nonce_TEST_ONLY, "base64url"),
      sha(Buffer.from("CIVORA v0.5 TEST_ONLY independence " + i)),
    ),
    binding = await scaleBinding(
      context,
      digest,
      sha(Buffer.from(canonical(independence))).toString("hex"),
    );
  const membership =
    old?.ballots[i]?.membership ??
    (await prove(identity, binding.message, binding.scope));
  if (
    membership.message !== binding.message ||
    membership.scope !== binding.scope ||
    membership.nullifier !== nullifier ||
    !(await verifyProof({ ...membership, merkleTreeDepth: 26 }))
  )
    throw Error("frozen proof diverged");
  const envelope = {
    network_id: context.network_id,
    poll_id: context.poll_id,
    protocol_epoch: "1",
    lane_id: await lane(context.network_id, context.poll_id, nullifier, "8"),
    checkpoint_id: context.checkpoint_id,
    encrypted_ballot: encrypted,
    independence,
    membership,
  };
  await verifyScale(election, context, envelope);
  ballots.push(envelope);
}
const attack = {
  ...scale.copy_attack,
  lane_id: await lane(
    context.network_id,
    context.poll_id,
    scale.copy_attack.membership.nullifier,
    "8",
  ),
};
await verifyScale(election, context, attack).then(
  () => {
    throw Error("copy attack accepted");
  },
  () => {},
);
const original = ballots[1],
  alternateBytes = decodeBundle(original.encrypted_ballot, "ballot").files.get(
    "ballot.json",
  ),
  alternateDigest = sha(alternateBytes).toString("hex"),
  alternateAlpha = JSON.parse(new TextDecoder().decode(alternateBytes))
    .contests[0].selections[0].encrypted_vote.alpha;
const alternateIndependence = await createIndependence(
    context,
    original.membership.nullifier,
    alternateDigest,
    alternateAlpha,
    Buffer.from(oracle.ballots[1].selection_nonce_TEST_ONLY, "base64url"),
    sha(Buffer.from("CIVORA v0.5 TEST_ONLY third distinct attempt")),
  ),
  alternateBinding = await scaleBinding(
    context,
    alternateDigest,
    sha(Buffer.from(canonical(alternateIndependence))).toString("hex"),
  );
const alternateMembership =
  old?.third_attempt?.membership ??
  (await prove(
    Identity.import(actors[1].identity_export_TEST_ONLY),
    alternateBinding.message,
    alternateBinding.scope,
  ));
const third_attempt = {
  ...original,
  independence: alternateIndependence,
  membership: alternateMembership,
};
await verifyScale(election, context, third_attempt);
const out = canonical({
  label: "PUBLIC_TEST_IDENTITIES_REAL_PROOFS_FROZEN_NOT_PUBLIC_QUALIFICATION",
  context,
  ballots,
  copy_attack: attack,
  third_attempt,
});
mkdirSync("spec/fixtures/v0.5", { recursive: true });
if (check) {
  if (readFileSync(path, "utf8") !== out)
    throw Error("stale partition ballots");
} else writeFileSync(path, out);
if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
