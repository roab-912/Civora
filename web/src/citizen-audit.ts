// Independent selective archive audit. No Go subprocess or node database.
import {
  blob,
  canonical,
  decode,
  domain,
  sha,
  uint,
  validate,
  verifyDigest,
  type Entry,
} from "./protocol.js";
import {
  logRoot,
  verifyConsistency,
  verifyHeader,
  verifyInclusion,
  verifyState,
  type Trust,
  type StateProof,
} from "./consensus.js";
import { stateRoot } from "./tree.js";
import { decodeBundle } from "./electionguard-bundle.js";
import {
  verifyCeremony,
  verifyBallot,
  pair,
  encodedPair,
  prime,
  zeroAggregate,
  addAggregate,
  verifyTally,
} from "./electionguard.js";
import {
  verifyIndependence,
  lane as route,
  scaleBinding,
  type ScaleContext,
  type ScaleEnvelope,
} from "./electionguard-scale.js";
import { verifyMembershipBound } from "./semaphore.js";
export interface Ref {
  sha256: string;
  size: string;
}
export interface Witness {
  entry: Entry;
  exists: boolean;
  siblings: string[];
}
export interface PublicProof {
  height: string;
  signed_header: string;
  path: Witness;
}
export interface AuditPolicy {
  keys: string[];
  implementations: string[];
}
export interface Frozen {
  context: ScaleContext;
  proposal_id: string;
  guardian_ids: string[];
  ceremony_bundle: string;
  opens_at_ms: string;
  closes_at_ms: string;
  source_height: string;
  lanes: Trust[];
  archive_keys: string[];
  eligible_count: string;
  audit?: AuditPolicy;
}
interface BatchRef {
  id: string;
  network_id: string;
  poll_id: string;
  lane_id: string;
  protocol_epoch: string;
  start: string;
  count: string;
  root: string;
  manifest_sha256: string;
  retention: string;
}
interface Signature {
  key: string;
  signature: string;
}
interface Batch {
  reference: BatchRef;
  envelopes: Ref[];
  signed_header: string;
  transaction_proof: string;
}
interface Summary {
  network_id: string;
  poll_id: string;
  lane_id: string;
  protocol_epoch: string;
  context_id: string;
  height: string;
  time_ms: string;
  admitted_count: string;
  admission_root: string;
  processed_count: string;
  status_root: string;
  counted_count: string;
  spent_root: string;
  aggregate: string;
  batch_count: string;
  batch_root: string;
  admission_sealed: boolean;
  processing_sealed: boolean;
}
interface Checkpoint {
  proof: StateProof;
  previous_id: string;
  admission_consistency: string[];
  batch_consistency: string[];
  extension: {
    reference: BatchRef;
    certificate: Signature[];
    inclusion: string[];
  }[];
}
interface Verified {
  id: string;
  summary: Summary;
}
interface Lane {
  lane_id: string;
  accepted: PublicProof;
  checkpoints: Checkpoint[];
  batches: Batch[];
}
export interface Result {
  aggregate: string;
  count: string;
  yes: string;
  no: string;
  state: string;
  decryption_record: string;
  dossier_id?: string;
  published_ms?: string;
  published_height?: string;
}
export interface ElectionRecord {
  format: "civic-election-1";
  genesis: Ref;
  frozen: Frozen;
  opening: StateProof;
  lanes: Lane[];
  result: PublicProof;
  status: PublicProof;
  certificate?: PublicProof;
}
export type ObjectLoader = (ref: Ref) => Uint8Array;
function eq(a: unknown, b: unknown) {
  if (canonical(a) !== canonical(b)) throw Error("archive linkage mismatch");
}
function hash(b: Uint8Array): string {
  return sha(b).toString("hex");
}
export function verifyWitness(root: string, p: Witness): void {
  validate("state_witness", p);
  if (!p.exists && p.entry.value !== null) throw Error("absent value");
  const k = Buffer.from(
    domain("state/key", [p.entry.table, p.entry.key]),
    "hex",
  );
  let h = p.exists
    ? sha(Buffer.from([0]), k, sha(Buffer.from(canonical(p.entry.value))))
    : sha(Buffer.from([2]));
  for (let i = 0; i < 256; i++) {
    const d = 255 - i,
      s = Buffer.from(p.siblings[i]!, "hex");
    h =
      k[d >> 3]! & (1 << (7 - (d % 8)))
        ? sha(Buffer.from([1]), s, h)
        : sha(Buffer.from([1]), h, s);
  }
  if (h.toString("hex") !== root.toLowerCase()) throw Error("witness root");
}
export function verifyPublicState(t: Trust, p: PublicProof) {
  validate("citizen_state_proof", p);
  const header = verifyHeader(t, p.signed_header);
  if (uint(p.height) === 0n || uint(header.height) !== uint(p.height) + 1n)
    throw Error("public state H+1");
  verifyWitness(header.app_hash, p.path);
  return header;
}
function nativeTransaction(t: Trust, header: string, encoded: string) {
  const h = verifyHeader(t, header),
    raw = blob(encoded);
  if (raw.length > 400 * 1024) throw Error("transaction proof bound");
  const p = JSON.parse(raw.toString()) as {
    root_hash: string;
    data: string;
    proof: { total: string; index: string; leaf_hash: string; aunts: string[] };
  };
  const data = Buffer.from(p.data, "base64"),
    total = uint(p.proof.total),
    index = uint(p.proof.index);
  if (data.length > 256 * 1024 || total < 1n || total > 8192n)
    throw Error("native proof bound");
  eq(p.root_hash.toLowerCase(), h.data_hash.toLowerCase());
  eq(
    Buffer.from(p.proof.leaf_hash, "base64").toString("hex"),
    sha(Buffer.from([0]), sha(data)).toString("hex"),
  );
  verifyInclusion(
    h.data_hash.toLowerCase(),
    sha(data),
    index,
    total,
    p.proof.aunts.map((s) => Buffer.from(s, "base64").toString("hex")),
  );
  return { header: h, data, index };
}
function refValues(r: BatchRef) {
  return [
    r.network_id,
    r.poll_id,
    r.lane_id,
    r.protocol_epoch,
    r.start,
    r.count,
    r.root,
    r.manifest_sha256,
    r.retention,
  ];
}
function certificate(f: Frozen, r: BatchRef, signatures: Signature[]) {
  validate("partition_batch_ref", r);
  eq(r.id, domain("scale/batch", refValues(r)));
  if (signatures.length < 3 || signatures.length > 5)
    throw Error("archive quorum");
  let previous = "";
  for (const s of signatures) {
    if (
      s.key <= previous ||
      !f.archive_keys.includes(s.key) ||
      !verifyDigest(domain("scale/archive", refValues(r)), s.signature, s.key)
    )
      throw Error("archive certificate");
    previous = s.key;
  }
}
function checkpoint(f: Frozen, c: Checkpoint, old?: Verified): Verified {
  validate("partition_checkpoint", c);
  const s = validate<Summary>("partition_summary", c.proof.path.entry.value),
    i = Number(uint(s.lane_id));
  if (!f.lanes[i]) throw Error("lane index");
  const header = verifyState(f.lanes[i]!, c.proof);
  eq(c.proof.path.entry.table, "lane_checkpoint");
  eq(c.proof.path.entry.key, f.context.poll_id);
  eq(s.height, c.proof.height);
  for (const [a, b] of [
    [s.network_id, f.context.network_id],
    [s.poll_id, f.context.poll_id],
    [s.protocol_epoch, f.context.protocol_epoch],
    [s.context_id, f.context.checkpoint_id],
  ])
    eq(a, b);
  const a = uint(s.admitted_count),
    p = uint(s.processed_count),
    count = uint(s.counted_count),
    b = uint(s.batch_count),
    time = uint(s.time_ms),
    closed = time >= uint(f.closes_at_ms);
  if (
    p > a ||
    count > p ||
    count > uint(f.eligible_count) ||
    a > 2n * uint(f.eligible_count) ||
    b > a ||
    s.admission_sealed !== closed ||
    s.processing_sealed !== (closed && p === a) ||
    time > BigInt(Date.parse(header.time))
  )
    throw Error("checkpoint counters/seals");
  if (a === 0n) eq(s.admission_root, logRoot([]));
  if (b === 0n) eq(s.batch_root, logRoot([]));
  if (p === 0n) eq(s.status_root, stateRoot([]));
  if (count === 0n) {
    eq(s.spent_root, stateRoot([]));
    eq(s.aggregate, zeroAggregate());
  }
  const before = old?.summary,
    oa = before ? uint(before.admitted_count) : 0n,
    ob = before ? uint(before.batch_count) : 0n;
  eq(c.previous_id, old?.id ?? "00".repeat(32));
  if (
    before &&
    (uint(s.height) <= uint(before.height) ||
      time < uint(before.time_ms) ||
      p < uint(before.processed_count) ||
      count < uint(before.counted_count) ||
      (before.admission_sealed && !s.admission_sealed) ||
      (before.processing_sealed && !s.processing_sealed))
  )
    throw Error("checkpoint regression");
  verifyConsistency(
    before?.admission_root ?? logRoot([]),
    s.admission_root,
    oa,
    a,
    c.admission_consistency,
  );
  verifyConsistency(
    before?.batch_root ?? logRoot([]),
    s.batch_root,
    ob,
    b,
    c.batch_consistency,
  );
  if (BigInt(c.extension.length) !== b - ob || c.extension.length > 64)
    throw Error("batch extension omitted");
  let next = oa;
  for (let j = 0; j < c.extension.length; j++) {
    const x = c.extension[j]!,
      r = x.reference;
    eq(
      [r.network_id, r.poll_id, r.lane_id, r.protocol_epoch],
      [s.network_id, s.poll_id, s.lane_id, s.protocol_epoch],
    );
    if (uint(r.start) !== next || uint(r.count) === 0n || uint(r.count) > 32n)
      throw Error("batch interval");
    next += uint(r.count);
    certificate(f, r, x.certificate);
    verifyInclusion(
      s.batch_root,
      Buffer.from(canonical(r)),
      ob + BigInt(j),
      b,
      x.inclusion,
    );
  }
  if (next !== a) throw Error("missing admitted batch");
  pair(JSON.parse(s.aggregate));
  return {
    id: domain("scale/checkpoint", [c.proof.height, c.proof.signed_header, s]),
    summary: s,
  };
}
function frozenID(f: Frozen) {
  const c = f.context,
    values: unknown[] = [
      c.network_id,
      c.poll_id,
      c.protocol_epoch,
      c.group_root,
      c.circuit_depth,
      c.partition_count,
      f.proposal_id,
      f.guardian_ids,
      f.ceremony_bundle,
      f.opens_at_ms,
      f.closes_at_ms,
      f.source_height,
      f.lanes,
      f.archive_keys,
      f.eligible_count,
    ];
  if (f.audit) values.push(f.audit);
  return domain("scale/open", values);
}
export async function auditRecord(
  record: ElectionRecord,
  load: ObjectLoader,
  expectedGenesis: string,
) {
  validate("citizen_election_record", record);
  if (!expectedGenesis || record.genesis.sha256 !== expectedGenesis)
    throw Error("authenticated genesis required");
  const object = (r: Ref) => {
    validate("archive_ref", r);
    const data = load(r);
    if (
      data.length !== Number(uint(r.size)) ||
      data.length > 32 * 1024 * 1024 ||
      hash(data) !== r.sha256
    )
      throw Error("object integrity");
    return data;
  };
  const genesis = JSON.parse(
    new TextDecoder().decode(object(record.genesis)),
  ) as {
    chain_id: string;
    validators: { pub_key: { type: string; value: string }; power: string }[];
    app_state: { consensus_rotations?: { height: string; keys: string[] }[] };
  };
  const initial: Trust = {
    chain_id: genesis.chain_id,
    keys: genesis.validators.map((v) => {
      if (v.power !== "1" || v.pub_key.type !== "tendermint/PubKeyEd25519")
        throw Error("genesis validators");
      return Buffer.from(v.pub_key.value, "base64").toString("hex");
    }),
  };
  const trustAt = (h: bigint) => {
    let keys = initial.keys;
    for (const r of genesis.app_state.consensus_rotations ?? [])
      if (h >= uint(r.height) + 2n) keys = r.keys;
    return { ...initial, keys };
  };
  const f = record.frozen,
    c = f.context;
  eq(c.checkpoint_id, frozenID(f));
  if (
    f.lanes.length !== Number(uint(c.partition_count)) ||
    f.lanes.length < 1 ||
    (f.lanes.length & (f.lanes.length - 1)) !== 0
  )
    throw Error("partition manifest");
  for (let i = 0; i < f.lanes.length; i++)
    eq(
      f.lanes[i]!.chain_id,
      genesis.chain_id + "-v" + i.toString(16).padStart(2, "0"),
    );
  verifyState(trustAt(uint(record.opening.height) + 1n), record.opening);
  eq(record.opening.height, f.source_height);
  eq(record.opening.path.entry, {
    table: "transport_polls",
    key: c.poll_id,
    value: f,
  });
  const resultH = uint(record.result.height);
  verifyPublicState(trustAt(resultH + 1n), record.result);
  if (!record.result.path.exists) throw Error("result absent");
  eq(record.result.path.entry.table, "transport_results");
  eq(record.result.path.entry.key, c.poll_id);
  const result = validate<Result>(
      "partition_provisional",
      record.result.path.entry.value,
    ),
    election = await verifyCeremony(
      {
        network_id: c.network_id,
        poll_id: c.poll_id,
        proposal_id: f.proposal_id,
        protocol_epoch: c.protocol_epoch,
        guardian_ids: f.guardian_ids,
      },
      f.ceremony_bundle,
    );
  if (record.lanes.length !== f.lanes.length) throw Error("missing lane");
  const final: Verified[] = [],
    aggregates: string[] = [];
  let total = 0n,
    admitted = 0n;
  for (let i = 0; i < record.lanes.length; i++) {
    const l = record.lanes[i]!;
    eq(l.lane_id, String(i));
    let last: Verified | undefined;
    for (const cp of l.checkpoints) last = checkpoint(f, cp, last);
    if (!last || !last.summary.processing_sealed) throw Error("lane unsealed");
    eq(last.summary.lane_id, l.lane_id);
    eq(l.accepted.height, record.result.height);
    verifyPublicState(trustAt(resultH + 1n), l.accepted);
    if (!l.accepted.path.exists) throw Error("checkpoint unanchored");
    eq(l.accepted.path.entry, {
      table: "transport_checkpoints",
      key: [c.poll_id, l.lane_id],
      value: last,
    });
    let aggregate = zeroAggregate(),
      position = 0n,
      count = 0n,
      previousHeight = 0n,
      previousTx = -1n;
    const spent = new Map<string, string>(),
      ballots = new Set<string>(),
      ciphertexts = new Set<string>(),
      statuses: Entry[] = [],
      items: Uint8Array[] = [],
      batchItems: Uint8Array[] = [];
    for (const batch of l.batches) {
      const ref = batch.reference;
      eq(ref.start, String(position));
      const tx = nativeTransaction(
          f.lanes[i]!,
          batch.signed_header,
          batch.transaction_proof,
        ),
        h = uint(tx.header.height),
        time = BigInt(Date.parse(tx.header.time));
      if (
        time < uint(f.opens_at_ms) ||
        time >= uint(f.closes_at_ms) ||
        h < previousHeight ||
        (h === previousHeight && tx.index <= previousTx)
      )
        throw Error("admission time/order");
      previousHeight = h;
      previousTx = tx.index;
      const body = decode<{
        type: string;
        value: {
          poll_id: string;
          envelopes: string[];
          certificate: Signature[];
        };
      }>("partition_tx", tx.data);
      eq(body.type, "partition.admit");
      eq(body.value.poll_id, c.poll_id);
      const data = batch.envelopes.map(object);
      eq(
        body.value.envelopes,
        data.map((d) => Buffer.from(d).toString("base64url")),
      );
      eq(ref.network_id, c.network_id);
      eq(ref.poll_id, c.poll_id);
      eq(ref.lane_id, l.lane_id);
      eq(ref.protocol_epoch, c.protocol_epoch);
      eq(ref.count, String(data.length));
      eq(ref.root, logRoot(data));
      eq(
        ref.manifest_sha256,
        hash(
          Buffer.from(
            canonical(
              data.map((d) => ({ sha256: hash(d), size: String(d.length) })),
            ),
          ),
        ),
      );
      certificate(f, ref, body.value.certificate);
      batchItems.push(Buffer.from(canonical(ref)));
      for (const raw of data) {
        const env = decode<ScaleEnvelope>("scale_envelope", raw);
        for (const k of [
          "network_id",
          "poll_id",
          "protocol_epoch",
          "checkpoint_id",
        ] as const)
          eq(env[k], c[k]);
        eq(env.lane_id, l.lane_id);
        eq(
          env.lane_id,
          await route(
            c.network_id,
            c.poll_id,
            env.membership.nullifier,
            c.partition_count,
          ),
        );
        const bundle = decodeBundle(env.encrypted_ballot, "ballot"),
          digest = hash(bundle.files.get("ballot.json")!),
          ind = hash(Buffer.from(canonical(env.independence)));
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
            env.membership,
            await scaleBinding(c, digest, ind),
          ))
        )
          throw Error("invalid admitted eligibility");
        let status = "invalid";
        try {
          const ballot = await verifyBallot(election, env.encrypted_ballot);
          await verifyIndependence(
            c,
            env.membership.nullifier,
            digest,
            ballot.ciphertext.alpha,
            env.independence,
          );
          const cipherID = domain("vote/ciphertext", ballot.ciphertext);
          if (
            spent.has(env.membership.nullifier) ||
            ballots.has(ballot.ballot_id) ||
            ciphertexts.has(cipherID)
          )
            status = "duplicate";
          else {
            aggregate = addAggregate(aggregate, ballot.ciphertext);
            spent.set(env.membership.nullifier, hash(raw));
            ballots.add(ballot.ballot_id);
            ciphertexts.add(cipherID);
            status = "counted";
            count++;
          }
        } catch {
          status = "invalid";
        }
        statuses.push({
          table: "status",
          key: String(position),
          value: { digest: hash(raw), status },
        });
        position++;
        items.push(raw);
      }
    }
    const s = last.summary;
    eq(
      [
        s.admitted_count,
        s.processed_count,
        s.counted_count,
        s.aggregate,
        s.admission_root,
        s.batch_count,
        s.batch_root,
        s.status_root,
        s.spent_root,
      ],
      [
        String(position),
        String(position),
        String(count),
        aggregate,
        logRoot(items),
        String(batchItems.length),
        logRoot(batchItems),
        stateRoot(statuses),
        stateRoot(
          [...spent].map(([key, value]) => ({ table: "spent", key, value })),
        ),
      ],
    );
    admitted += position;
    total += count;
    aggregates.push(aggregate);
    final.push(last);
  }
  let alpha = 1n,
    beta = 1n;
  for (const aggregate of aggregates) {
    const [a, b] = pair(JSON.parse(aggregate));
    alpha = (alpha * a) % prime;
    beta = (beta * b) % prime;
  }
  const aggregate = canonical(encodedPair(alpha, beta));
  eq(result.aggregate, aggregate);
  eq(result.count, String(total));
  if (total > uint(f.eligible_count)) throw Error("global count");
  if (total === 0n)
    eq([result.yes, result.no, result.decryption_record], ["0", "0", ""]);
  else {
    const total = await verifyTally(
      election,
      aggregate,
      result.count,
      result.decryption_record,
    );
    eq([result.yes, result.no], [total.yes, total.no]);
  }
  const dossier = domain("audit/dossier", [
    c.network_id,
    c.poll_id,
    c.protocol_epoch,
    c.checkpoint_id,
    final,
    aggregate,
    result.count,
    result.yes,
    result.no,
    hash(blob(result.decryption_record)),
  ]);
  if (f.audit) eq(result.dossier_id, dossier);
  let status = result.state;
  if (record.certificate) {
    const p = record.certificate;
    verifyPublicState(trustAt(uint(p.height) + 1n), p);
    if (!f.audit || !p.path.exists)
      throw Error("certificate authority/absence");
    eq(
      [p.path.entry.table, p.path.entry.key],
      ["poll_certificates", c.poll_id],
    );
    const cert = validate<{
      poll_id: string;
      dossier_id: string;
      published_ms: string;
      certified_height: string;
      certified_ms: string;
      attestations: {
        poll_id: string;
        dossier_id: string;
        implementation: string;
        signer: string;
        signature: string;
      }[];
    }>("audit_certification", p.path.entry.value);
    eq(
      [cert.poll_id, cert.dossier_id, cert.published_ms, cert.certified_height],
      [c.poll_id, dossier, result.published_ms, p.height],
    );
    const tm = uint(cert.certified_ms);
    if (
      tm < uint(cert.published_ms) + 1800000n ||
      tm >= uint(f.closes_at_ms) + 172800000n
    )
      throw Error("certificate time window");
    const signers = new Set<string>(),
      implementations = new Set<string>();
    for (const a of cert.attestations) {
      if (
        signers.has(a.signer) ||
        !f.audit.keys.includes(a.signer) ||
        !f.audit.implementations.includes(a.implementation)
      )
        throw Error("audit authority/duplicate");
      eq([a.poll_id, a.dossier_id], [c.poll_id, dossier]);
      if (
        !verifyDigest(
          domain("audit/attest", [
            c.network_id,
            c.poll_id,
            c.protocol_epoch,
            dossier,
            a.implementation,
          ]),
          a.signature,
          a.signer,
        )
      )
        throw Error("audit signature");
      signers.add(a.signer);
      implementations.add(a.implementation);
    }
    if (signers.size < 3 || implementations.size < 2)
      throw Error("independent audit quorum");
    status = "certified";
  }
  const current = record.status;
  if (uint(current.height) < resultH) throw Error("status predates dossier");
  verifyPublicState(trustAt(uint(current.height) + 1n), current);
  if (!current.path.exists) throw Error("status absent");
  eq(
    [current.path.entry.table, current.path.entry.key],
    ["transport_results", c.poll_id],
  );
  const latest = validate<Result>(
      "partition_provisional",
      current.path.entry.value,
    ),
    latestState = latest.state;
  eq({ ...latest, state: result.state }, result);
  if (latestState === "contested" || latestState === "failed_certification")
    status = latestState;
  if (
    record.certificate &&
    uint(current.height) < uint(record.certificate.height)
  )
    throw Error("status predates certificate");
  return {
    network_id: c.network_id,
    poll_id: c.poll_id,
    dossier_id: dossier,
    admitted: String(admitted),
    counted: String(total),
    yes: result.yes,
    no: result.no,
    state: status,
    verifier: "civic-typescript-v0.6",
    verified: true,
    as_of_height: current.height,
  };
}
