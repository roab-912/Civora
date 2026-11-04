// PUBLIC TEST IDENTITIES ONLY. Groth16 proofs are probabilistic; existing
// reviewed proof bytes are reused and verified unless --new-proofs is explicit.
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { Identity } from "@semaphore-protocol/identity";
import { Group } from "@semaphore-protocol/group";
import { generateProof, verifyProof } from "@semaphore-protocol/proof";
import { canonical, domain } from "../dist/web/src/protocol.js";
import { binding } from "../dist/web/src/eligibility.js";
const check = process.argv.includes("--check"),
  newProofs = process.argv.includes("--new-proofs");
if (check && newProofs) throw Error("Cannot regenerate proofs in check mode");
const folder = "spec/fixtures/v0.3";
mkdirSync(folder, { recursive: true });
const write = (name, data) => {
  const path = folder + "/" + name + ".json",
    output = canonical(data);
  if (check) {
    if (readFileSync(path, "utf8") !== output)
      throw Error("stale fixture: " + path);
  } else writeFileSync(path, output);
};
const ids = Array.from(
  { length: 9 },
  (_, i) =>
    new Identity(
      createHash("sha256")
        .update("CIVORA PUBLIC SEMAPHORE TEST IDENTITY v0.3 " + i)
        .digest(),
    ),
);
write("identities.TEST_ONLY", {
  warning: "PUBLIC TEST SECRETS. NEVER USE FOR REAL ACCOUNTS OR VOTES.",
  identities: ids.map((i) => ({
    identity_export_TEST_ONLY: i.export(),
    commitment: i.commitment.toString(),
  })),
});
const network = domain("eligibility/test-network", "PUBLIC TEST ONLY v0.3");
function snapshot(group, height, depth = "16") {
  const m = {
    network_id: network,
    protocol_epoch: "1",
    tree_id: "sdk-cross-vector",
    suite_id: "semaphore-v4.14.5-depth-" + depth,
    height: String(height),
    circuit_depth: depth,
    size: String(group.size),
    active_count: String(group.members.filter((v) => v !== 0n).length),
    root: group.size === 0 ? null : group.root.toString(),
  };
  return {
    ...m,
    snapshot_id: domain("eligibility/snapshot", [
      m.network_id,
      m.protocol_epoch,
      m.tree_id,
      m.suite_id,
      m.height,
      m.circuit_depth,
      m.size,
      m.active_count,
      m.root,
    ]),
  };
}
function path(group, index, m) {
  const p = group.generateMerkleProof(index);
  return {
    snapshot: m,
    leaf_index: String(index),
    leaf: p.leaf.toString(),
    index: String(p.index),
    siblings: p.siblings.map(String),
  };
}
const group = new Group(),
  steps = [];
let height = 0;
const record = (changes) => {
  height++;
  const m = snapshot(group, height);
  steps.push({
    changes,
    snapshot: m,
    paths: group.members.flatMap((v, i) =>
      v === 0n ? [] : [path(group, i, m)],
    ),
  });
};
for (let i = 0; i < 5; i++) {
  group.addMember(ids[i].commitment);
  record([
    {
      operation: "append",
      index: String(i),
      commitment: ids[i].commitment.toString(),
    },
  ]);
}
group.removeMember(1);
record([{ operation: "update", index: "1", commitment: "0" }]);
group.removeMember(0);
group.addMember(ids[5].commitment);
record([
  { operation: "update", index: "0", commitment: "0" },
  { operation: "append", index: "5", commitment: ids[5].commitment.toString() },
]);
group.addMembers(ids.slice(6).map((i) => i.commitment));
record(
  ids.slice(6).map((id, i) => ({
    operation: "append",
    index: String(i + 6),
    commitment: id.commitment.toString(),
  })),
);
write("lean-imt", { network_id: network, steps });
const lock = JSON.parse(
  readFileSync("spec/crypto/semaphore-v4.lock.json", "utf8"),
);
for (const depth of ["16", "26"]) {
  const g = new Group(ids.slice(0, 5).map((i) => i.commitment)),
    m = snapshot(g, 1, depth),
    p = path(g, 4, m);
  const context = {
    network_id: network,
    poll_id: domain("test/poll", "Semaphore v0.3 " + depth),
    protocol_epoch: "1",
    suite_id: m.suite_id,
    circuit_depth: depth,
    group_root: m.root,
  };
  const ballot = Buffer.from(
    "PUBLIC DEVELOPMENT BYTES — ciphertext binding only, ElectionGuard is v0.4",
  );
  const { scope, message } = await binding(
    context.network_id,
    context.poll_id,
    ballot,
  );
  let membership;
  const target = folder + "/membership-" + depth + ".json";
  if (existsSync(target) && !newProofs) {
    membership = JSON.parse(readFileSync(target, "utf8")).membership;
  } else {
    const artifacts = lock.depths.find((v) => v.depth === depth);
    for (const kind of ["wasm", "zkey"]) {
      const file = readFileSync(".tools/semaphore/" + artifacts[kind].file);
      if (
        createHash("sha256").update(file).digest("hex") !==
          artifacts[kind].sha256 ||
        String(file.length) !== artifacts[kind].size
      )
        throw Error("ERR_ARTIFACT_HASH");
    }
    const proof = await generateProof(
      ids[4],
      {
        root: g.root,
        leaf: ids[4].commitment,
        index: Number(p.index),
        siblings: p.siblings.map(BigInt),
      },
      message,
      scope,
      Number(depth),
      {
        wasm: ".tools/semaphore/" + depth + "/semaphore.wasm",
        zkey: ".tools/semaphore/" + depth + "/semaphore.zkey",
      },
    );
    membership = { ...proof, merkleTreeDepth: depth };
  }
  if (
    membership.merkleTreeRoot !== m.root ||
    membership.message !== message ||
    membership.scope !== scope ||
    !(await verifyProof({ ...membership, merkleTreeDepth: Number(depth) }))
  )
    throw Error("invalid stored membership proof");
  write("membership-" + depth, {
    warning:
      "PUBLIC DEVELOPMENT IDENTITY. No ElectionGuard ballot is implemented in v0.3.",
    context,
    path: p,
    identity_export_TEST_ONLY: ids[4].export(),
    ballot_base64url: ballot.toString("base64url"),
    membership,
  });
}
if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
