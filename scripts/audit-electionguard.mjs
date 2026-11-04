// Independent public crypto audit. Governance transitions are replayed by the
// Go auditor; this verifier additionally recomputes each archived state root.
import { readFileSync } from "node:fs";
import { decode, domain, sha } from "../dist/web/src/protocol.js";
import { stateRoot } from "../dist/web/src/tree.js";
import { verifyMembership } from "../dist/web/src/semaphore.js";
import {
  verifyCeremony,
  verifyBallot,
  verifyTally,
  zeroAggregate,
  addAggregate,
} from "../dist/web/src/electionguard.js";
const i = process.argv.indexOf("-in");
if (i < 0 || !process.argv[i + 1])
  throw Error(
    "usage: node scripts/audit-electionguard.mjs -in public-archive.json",
  );
const raw = readFileSync(process.argv[i + 1]);
if (raw.length > 64 * 1024 * 1024) throw Error("archive too large");
const archive = decode("eg_archive", raw),
  final = archive.records.at(-1);
if (!final) throw Error("empty archive has no result to audit");
for (const record of archive.records)
  if (stateRoot(record.state.entries) !== record.app_hash)
    throw Error("archived state root mismatch");
const txs = archive.records.flatMap((r) => r.block.transactions),
  polls = final.state.entries.filter((e) => e.table === "polls");
const verified = [];
try {
  for (const { key: poll_id, value: poll } of polls) {
    if (!poll.result) continue;
    const proposal = final.state.entries.find(
      (e) => e.table === "proposals" && e.key === poll.proposal_id,
    )?.value;
    const context = {
      network_id: archive.genesis.network_id,
      poll_id,
      proposal_id: poll.proposal_id,
      protocol_epoch: poll.protocol_epoch,
      guardian_ids: proposal.guardians.map((g) => g.account_id).sort(),
    };
    const election = await verifyCeremony(context, poll.ceremony_bundle);
    let aggregate = zeroAggregate(),
      count = 0;
    const nullifiers = new Set(),
      ids = new Set(),
      ciphertexts = new Set();
    for (const tx of txs.filter(
      (t) => t.body.type === "vote.cast" && t.body.payload.poll_id === poll_id,
    )) {
      const p = tx.body.payload,
        b = await verifyBallot(election, p.encrypted_ballot);
      if (
        !(await verifyMembership(
          {
            network_id: context.network_id,
            poll_id,
            protocol_epoch: poll.protocol_epoch,
            group_root: poll.group_root,
            circuit_depth: p.membership.merkleTreeDepth,
            suite_id: poll.eligibility_suite,
          },
          Buffer.from(p.encrypted_ballot, "base64url"),
          p.membership,
        ))
      )
        throw Error("invalid archived eligibility");
      const ciphertext = domain("vote/ciphertext", b.ciphertext);
      if (
        nullifiers.has(p.membership.nullifier) ||
        ids.has(b.ballot_id) ||
        ciphertexts.has(ciphertext)
      )
        throw Error("duplicate archived ballot");
      nullifiers.add(p.membership.nullifier);
      ids.add(b.ballot_id);
      ciphertexts.add(ciphertext);
      aggregate = addAggregate(aggregate, b.ciphertext);
      count++;
    }
    if (String(count) !== poll.count || aggregate !== poll.aggregate)
      throw Error("archived aggregate/count mismatch");
    const tally = txs.find(
      (t) => t.body.type === "poll.tally" && t.body.payload.poll_id === poll_id,
    )?.body.payload;
    if (count > 0) {
      if (
        !tally ||
        tally.aggregate_digest !==
          domain("poll/aggregate", [
            context.network_id,
            poll_id,
            poll.closed_height,
            String(count),
            aggregate,
          ])
      )
        throw Error("tally close binding mismatch");
      const result = await verifyTally(
        election,
        aggregate,
        String(count),
        tally.decryption_record,
      );
      if (result.yes !== tally.yes || result.no !== tally.no)
        throw Error("public tally differs");
    }
    verified.push(poll_id);
  }
  console.log(
    JSON.stringify({
      crypto_verified: true,
      application_transitions_replayed: false,
      app_hash: final.app_hash,
      verified_results: verified.toSorted(),
    }),
  );
} finally {
  if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
}
