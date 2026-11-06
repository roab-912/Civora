// Browser worker acceptance with a PUBLIC_TEST_ONLY identity, a historical
// authenticated opening and only its membership path. Never submits a ballot.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { verifyCeremony } from "../dist/web/src/electionguard.js";
import { verifyScale } from "../dist/web/src/electionguard-scale.js";

export async function testPartitionWorker(page, origin, choice, pollID) {
  const base = origin + choice.api_roots[0],
    get = async (path) => {
      const r = await fetch(base + path);
      assert(r.ok);
      return r.json();
    },
    genesis = await (await fetch(base + "/v1/genesis")).text(),
    head = BigInt((await get("/v1/network")).height),
    proof = (table, key, height) =>
      get(
        "/v1/proofs/state?table=" +
          table +
          "&height=" +
          height +
          "&key=" +
          Buffer.from(JSON.stringify(key)).toString("base64url"),
      ),
    frozen = await proof("transport_polls", pollID, String(head - 1n)),
    f = frozen.path.entry.value;
  let configuration, height;
  for (let h = BigInt(f.source_height); h < head; h++) {
    const p = await proof("network", "config", String(h)),
      tm = BigInt(p.path.entry.value.time_ms);
    if (tm >= BigInt(f.opens_at_ms) && tm < BigInt(f.closes_at_ms)) {
      configuration = p;
      height = String(h);
      break;
    }
  }
  assert(height, "historical authenticated open phase missing");
  const context = {
      network_id: choice.manifest.network_id,
      poll_id: pollID,
      genesis,
      genesis_sha256: choice.manifest.main.genesis.sha256,
      configuration,
      proposal: await proof("proposals", f.proposal_id, height),
      frozen: await proof("transport_polls", pollID, height),
    },
    path = await get("/v1/polls/" + pollID + "/membership-path?leaf_index=0"),
    actors = JSON.parse(
      readFileSync("spec/fixtures/v0.4/actors.TEST_ONLY.json"),
    ).contexts[0].actors,
    actor = actors.find((a) => a.commitment === path.leaf);
  assert(actor, "path must match an explicitly identified test identity");
  const request = {
    context,
    path,
    identity: actor.identity_export_TEST_ONLY,
    choice: 1,
  };
  const run = () =>
    page.evaluate(
      (input) =>
        new Promise((yes, no) => {
          const w = new Worker("/worker.js", { type: "module" }),
            timer = setTimeout(() => {
              w.terminate();
              no(Error("partition worker timeout"));
            }, 120000);
          w.onmessage = (e) => {
            clearTimeout(timer);
            w.terminate();
            yes(e.data);
          };
          w.onerror = (e) => {
            clearTimeout(timer);
            w.terminate();
            no(Error(e.message));
          };
          w.postMessage({
            id: "PUBLIC_TEST_ONLY_PARTITION",
            type: "vote",
            input,
          });
        }),
      request,
    );
  const pattern = "**/crypto/semaphore/26/semaphore.wasm";
  await page
    .context()
    .route(pattern, (route) =>
      route.fulfill({ contentType: "application/wasm", body: "corrupt" }),
    );
  try {
    assert.equal((await run()).error, "ERR_ARTIFACT_HASH");
  } finally {
    await page.context().unroute(pattern);
  }
  const result = await run();
  assert(!result.error, result.error);
  assert.equal(result.result.kind, "partition");
  const envelope = JSON.parse(result.result.raw),
    proposal = context.proposal.path.entry.value,
    e = await verifyCeremony(
      {
        network_id: context.network_id,
        poll_id: pollID,
        proposal_id: f.proposal_id,
        protocol_epoch: f.context.protocol_epoch,
        guardian_ids: proposal.guardians.map((g) => g.account_id).sort(),
      },
      f.ceremony_bundle,
    );
  await verifyScale(e, f.context, envelope);
  assert(!result.result.raw.includes(actor.account_id));
  assert(!result.result.raw.includes('"choice"'));
  return [
    "partition-worker-refuses-corrupt-pinned-artifact",
    "partition-worker-encrypts-depth26-and-binds-nonce-proof-to-real-Semaphore-nullifier",
  ];
}
