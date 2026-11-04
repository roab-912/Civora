// The context supplied here must already be authenticated by a ledger verifier.
// Successful verification never consumes a nullifier or certifies a BFT receipt.
import { canonical } from "./codec.js";
import {
  decimal,
  domain,
  scalarModulus,
  sha256,
  type Membership,
} from "./eligibility.js";
import { verifyMembershipBound } from "./semaphore.js";
import {
  bytes,
  concat,
  decodeFixed,
  encodeBytes,
  exact,
  generator,
  group,
  index,
  integer,
  mod,
  order,
  power,
  prime,
  scalar,
  verifyBallot,
  type PublicElection,
} from "./electionguard.js";
export interface ScaleContext {
  network_id: string;
  poll_id: string;
  protocol_epoch: string;
  group_root: string;
  circuit_depth: "16" | "26";
  partition_count: string;
  checkpoint_id: string;
}
export interface Independence {
  commitment: string;
  response: string;
}
export interface ScaleEnvelope {
  network_id: string;
  poll_id: string;
  protocol_epoch: string;
  lane_id: string;
  checkpoint_id: string;
  encrypted_ballot: string;
  independence: Independence;
  membership: Membership;
}
export async function lane(
  network: string,
  poll: string,
  nullifier: string,
  count: string,
): Promise<string> {
  decimal(nullifier, scalarModulus - 1n);
  const p = decimal(count, 256n);
  if (p < 1n || (p & (p - 1n)) !== 0n) throw Error("ERR_SCALE_PARTITIONS");
  let bits = 0n;
  for (let n = p; n > 1n; n >>= 1n) bits++;
  return String(
    BigInt(
      "0x" +
        (await domain("scale/lane", [network, poll, nullifier])).slice(0, 2),
    ) >>
      (8n - bits),
  );
}
export async function scaleBinding(
  c: ScaleContext,
  ballotDigest: string,
  independenceDigest: string,
) {
  return {
    scope: BigInt(
      "0x" +
        (await domain("scale/scope", [c.network_id, c.poll_id])).slice(0, 62),
    ).toString(),
    message: BigInt(
      "0x" +
        (
          await domain("scale/message", [
            c.network_id,
            c.poll_id,
            ballotDigest,
            independenceDigest,
          ])
        ).slice(0, 62),
    ).toString(),
  };
}
async function challenge(
  c: ScaleContext,
  nullifier: string,
  digest: string,
  a: bigint,
  commitment: bigint,
): Promise<bigint> {
  const ctx = await domain("scale/independence", [
    c.network_id,
    c.poll_id,
    c.group_root,
    nullifier,
    digest,
  ]);
  const parts = [
    bytes(generator),
    bytes(commitment),
    bytes(a),
    new TextEncoder().encode("eg-2.0-civic-1:" + nullifier),
    Uint8Array.from(ctx.match(/../g)!, (x) => parseInt(x, 16)),
  ];
  return (
    BigInt(
      "0x" +
        (await sha256(concat(...parts.flatMap((p) => [index(p.length), p])))),
    ) % order
  );
}
export async function verifyIndependence(
  c: ScaleContext,
  nullifier: string,
  digest: string,
  alpha: string,
  proof: Independence,
): Promise<void> {
  exact(proof, ["commitment", "response"]);
  const a = group(alpha, false),
    v = group(proof.commitment, false),
    r = scalar(proof.response);
  if (
    (power(generator, r) *
      power(a, await challenge(c, nullifier, digest, a, v))) %
      prime !==
    v
  )
    throw Error("ERR_SCALE_INDEPENDENCE");
}
// Development client boundary for the nonce exported by the pinned SDK. The
// input buffer is consumed and wiped. JS BigInt temporaries cannot be promised
// secure erasure; production client hardening remains subject to the v0.9 audit.
export async function createIndependence(
  c: ScaleContext,
  nullifier: string,
  digest: string,
  alpha: string,
  nonce: Uint8Array,
  randomness: Uint8Array,
): Promise<Independence> {
  try {
    const r = scalar(encodeBytes(nonce)),
      u = scalar(encodeBytes(randomness)),
      a = group(alpha, false);
    if (r === 0n || u === 0n || power(generator, r) !== a)
      throw Error("ERR_SELECTION_NONCE");
    const commitment = power(generator, u),
      response = mod(
        u - (await challenge(c, nullifier, digest, a, commitment)) * r,
      );
    return {
      commitment: encodeBytes(bytes(commitment)),
      response: encodeBytes(bytes(response, 32)),
    };
  } finally {
    nonce.fill(0);
    randomness.fill(0);
  }
}
export async function verifyScale(
  e: PublicElection,
  c: ScaleContext,
  envelope: ScaleEnvelope,
): Promise<void> {
  if (
    e.context.network_id !== c.network_id ||
    e.context.poll_id !== c.poll_id ||
    e.context.protocol_epoch !== c.protocol_epoch
  )
    throw Error("ERR_SCALE_CONTEXT");
  exact(c, [
    "network_id",
    "poll_id",
    "protocol_epoch",
    "group_root",
    "circuit_depth",
    "partition_count",
    "checkpoint_id",
  ]);
  exact(envelope, [
    "network_id",
    "poll_id",
    "protocol_epoch",
    "lane_id",
    "checkpoint_id",
    "encrypted_ballot",
    "independence",
    "membership",
  ]);
  if (
    !["16", "26"].includes(c.circuit_depth) ||
    decimal(c.protocol_epoch, 0xffffffffffffffffn) === 0n ||
    ![c.network_id, c.poll_id, c.checkpoint_id].every((x) =>
      /^[0-9a-f]{64}$/.test(x),
    )
  )
    throw Error("ERR_SCALE_CONTEXT");
  decimal(c.group_root, scalarModulus - 1n);
  for (const k of [
    "network_id",
    "poll_id",
    "protocol_epoch",
    "checkpoint_id",
  ] as const)
    if (envelope[k] !== c[k]) throw Error("ERR_SCALE_CONTEXT");
  if (
    envelope.lane_id !==
    (await lane(
      c.network_id,
      c.poll_id,
      envelope.membership.nullifier,
      c.partition_count,
    ))
  )
    throw Error("ERR_SCALE_LANE");
  const b = await verifyBallot(e, envelope.encrypted_ballot),
    bh = await sha256(b.raw),
    ih = await sha256(
      new TextEncoder().encode(canonical(envelope.independence)),
    );
  await verifyIndependence(
    c,
    envelope.membership.nullifier,
    bh,
    b.ciphertext.alpha,
    envelope.independence,
  );
  if (
    !(await verifyMembershipBound(
      {
        network_id: c.network_id,
        poll_id: c.poll_id,
        protocol_epoch: c.protocol_epoch,
        group_root: c.group_root,
        circuit_depth: c.circuit_depth,
        suite_id: "semaphore-v4.14.5-depth-" + c.circuit_depth,
      },
      envelope.membership,
      await scaleBinding(c, bh, ih),
    ))
  )
    throw Error("ERR_SCALE_MEMBERSHIP");
}
