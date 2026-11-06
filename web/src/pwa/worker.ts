/// <reference lib="webworker" />
import * as schemas from "civic-validators";
import { Identity } from "@semaphore-protocol/identity";
import { generateProof } from "@semaphore-protocol/proof";
import { poseidon2 } from "poseidon-lite/poseidon2";
import { keccak_256 } from "@noble/hashes/sha3";
import { Buffer } from "./buffer.js";
import {
  installValidators,
  canonical,
  domain,
  sha,
  validate,
} from "./protocol.js";
import { inspectContext, type VerifiedContext, type Row } from "./network.js";
import {
  verifyPath,
  binding,
  sha256,
  type EligibilityPath,
  type Snapshot,
  type Membership,
} from "../eligibility.js";
import { semaphoreLock } from "../semaphore-lock.js";
import {
  verifyCeremony,
  verifyTally,
  bytes,
  integer,
  encodeBytes,
} from "../electionguard.js";
import {
  createIndependence,
  scaleBinding,
  lane,
  verifyScale,
} from "../electionguard-scale.js";
import { verifyMembership } from "../semaphore.js";
import { encryptVote, randomScalar } from "./encrypt.js";
import {
  auditRecord,
  type ElectionRecord,
  type Ref,
} from "../citizen-audit.js";
installValidators(schemas);
const artifacts = new Map<string, { wasm: Uint8Array; zkey: Uint8Array }>();
async function load(depth: string) {
  const cached = artifacts.get(depth);
  if (cached) return cached;
  const record = semaphoreLock.depths.find((d) => d.depth === depth);
  if (!record) throw Error("ERR_SUITE");
  const data = await Promise.all(
    (["wasm", "zkey", "verification_key"] as const).map(async (kind) => {
      const info = record[kind],
        name =
          kind === "verification_key"
            ? "verification-key.json"
            : "semaphore." + kind,
        res = await fetch("/crypto/semaphore/" + depth + "/" + name, {
          credentials: "omit",
          cache: "no-store",
        });
      if (!res.ok) throw Error("ERR_ARTIFACT_FETCH");
      const raw = new Uint8Array(await res.arrayBuffer());
      if (
        String(raw.length) !== info.size ||
        (await sha256(raw)) !== info.sha256
      )
        throw Error("ERR_ARTIFACT_HASH");
      return raw;
    }),
  );
  const value = { wasm: data[0]!, zkey: data[1]! };
  artifacts.set(depth, value);
  return value;
}
export interface VoteRequest {
  context: VerifiedContext;
  identity: string;
  path: EligibilityPath;
  choice: 0 | 1;
}
async function vote(req: VoteRequest) {
  const { poll, proposal, config, frozen } = inspectContext(req.context);
  if (poll.state !== "open") throw Error("ERR_PHASE");
  const identity = Identity.import(req.identity),
    snapshot = req.path.snapshot as Snapshot;
  if (
    snapshot.network_id !== req.context.network_id ||
    snapshot.protocol_epoch !== poll.protocol_epoch ||
    snapshot.root !== poll.group_root ||
    snapshot.size !== poll.eligible_count ||
    snapshot.circuit_depth !== config.eligibility.circuit_depth ||
    snapshot.tree_id !== "poll-" + req.context.poll_id ||
    snapshot.height !== (frozen?.source_height ?? poll.snapshot_height)
  )
    throw Error("ERR_SNAPSHOT_CONTEXT");
  await verifyPath(req.path, snapshot, identity.commitment.toString());
  const e = await verifyCeremony(
      {
        network_id: req.context.network_id,
        poll_id: req.context.poll_id,
        proposal_id: poll.proposal_id,
        protocol_epoch: poll.protocol_epoch,
        guardian_ids: proposal.guardians.map((g: Row) => g.account_id).sort(),
      },
      poll.ceremony_bundle,
    ),
    encrypted = await encryptVote(e, req.choice);
  let expected = await binding(
      req.context.network_id,
      req.context.poll_id,
      Buffer.from(encrypted.encoded, "base64url"),
    ),
    independence;
  if (frozen) {
    const scope = (
        await scaleBinding(frozen.context, "0".repeat(64), "0".repeat(64))
      ).scope,
      scopeHash = integer(keccak_256(bytes(BigInt(scope), 32))) >> 8n,
      nullifier = poseidon2([scopeHash, identity.secretScalar]).toString();
    independence = await createIndependence(
      frozen.context,
      nullifier,
      await sha256(encrypted.raw),
      encrypted.alpha,
      encrypted.nonce,
      bytes(randomScalar(), 32),
    );
    expected = await scaleBinding(
      frozen.context,
      await sha256(encrypted.raw),
      await sha256(new TextEncoder().encode(canonical(independence))),
    );
  }
  encrypted.nonce.fill(0);
  const proof = await generateProof(
      identity,
      {
        root: BigInt(snapshot.root!),
        leaf: identity.commitment,
        index: Number(req.path.index),
        siblings: req.path.siblings.map(BigInt),
      },
      expected.message,
      expected.scope,
      Number(snapshot.circuit_depth),
      await load(snapshot.circuit_depth),
    ),
    membership: Membership = {
      ...proof,
      merkleTreeDepth: snapshot.circuit_depth,
      points: [...proof.points],
    };
  if (frozen) {
    const envelope = {
      ...frozen.context,
      lane_id: await lane(
        req.context.network_id,
        req.context.poll_id,
        membership.nullifier,
        frozen.context.partition_count,
      ),
      encrypted_ballot: encrypted.encoded,
      independence: independence!,
      membership,
    } as Row;
    delete envelope.group_root;
    delete envelope.circuit_depth;
    delete envelope.partition_count;
    validate("scale_envelope", envelope);
    await verifyScale(e, frozen.context, envelope as any);
    const raw = canonical(envelope);
    return {
      raw,
      digest: sha(Buffer.from(raw)).toString("hex"),
      lane_id: envelope.lane_id,
      kind: "partition",
    };
  }
  const context = {
    network_id: req.context.network_id,
    poll_id: req.context.poll_id,
    protocol_epoch: poll.protocol_epoch,
    suite_id: poll.eligibility_suite,
    circuit_depth: snapshot.circuit_depth,
    group_root: poll.group_root,
  };
  if (
    !(await verifyMembership(
      context,
      Buffer.from(encrypted.encoded, "base64url"),
      membership,
    ))
  )
    throw Error("ERR_GENERATED_PROOF");
  const body: Row = {
    v: config.registry.transaction_version,
    network_id: req.context.network_id,
    type: "vote.cast",
    actor_id: null,
    nonce: null,
    payload: {
      poll_id: req.context.poll_id,
      encrypted_ballot: encrypted.encoded,
      membership,
    },
  };
  if (body.v === "2") body.protocol_epoch = poll.protocol_epoch;
  const tx = { body, auth: { scheme: "proof" } };
  validate("transaction", tx);
  const raw = canonical(tx);
  return {
    raw,
    tx_id: domain(body.v === "1" ? "tx" : "tx/2", body),
    digest: sha(Buffer.from(raw)).toString("hex"),
    kind: "inline",
  };
}
let queue = Promise.resolve();
const worker = self as unknown as DedicatedWorkerGlobalScope;
worker.onmessage = (event) => {
  queue = queue.then(async () => {
    const { id, type, input } = event.data;
    try {
      let result: unknown;
      if (type === "vote") result = await vote(input as VoteRequest);
      else if (type === "result") {
        const { poll, proposal } = inspectContext(input.context);
        if (!poll.result || !poll.closed_height)
          throw Error("ERR_RESULT_PENDING");
        const e = await verifyCeremony(
          {
            network_id: input.context.network_id,
            poll_id: input.context.poll_id,
            proposal_id: poll.proposal_id,
            protocol_epoch: poll.protocol_epoch,
            guardian_ids: proposal.guardians
              .map((g: Row) => g.account_id)
              .sort(),
          },
          poll.ceremony_bundle,
        );
        result = await verifyTally(e, poll.aggregate, poll.count, input.tally);
        if (
          (result as Row).yes !== poll.result.yes ||
          (result as Row).no !== poll.result.no
        )
          throw Error("ERR_RESULT");
      } else if (type === "audit") {
        const files = new Map<string, Uint8Array>(input.objects);
        result = await auditRecord(
          input.record as ElectionRecord,
          (ref: Ref) => {
            const b = files.get(ref.sha256);
            if (!b) throw Error("ERR_RECORD_INCOMPLETE");
            return b;
          },
          input.genesis,
        );
      } else throw Error("ERR_REQUEST");
      worker.postMessage({ id, result });
    } catch (e) {
      worker.postMessage({
        id,
        error: e instanceof Error ? e.message : "ERR_PROOF",
      });
    }
  });
};
