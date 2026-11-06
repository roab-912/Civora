// Explicit local-development guardians. These secrets are publicly derivable;
// never load this module in the PWA or a public node.
import { readFileSync } from "node:fs";
import {
  canonical,
  domain,
  sha,
  signDigest,
  testKey,
} from "../dist/web/src/protocol.js";
import {
  hash,
  bytes,
  index,
  integer,
  mod,
  power,
  prime,
  order,
  generator,
  decodeFixed,
  encodeBytes,
  verifyCeremony,
  verifyTally,
  pair,
} from "../dist/web/src/electionguard.js";
import { electionguardParameters as pin } from "../dist/web/src/electionguard-parameters.js";
const enc = (n) => encodeBytes(bytes(n, 32)),
  group = (n) => encodeBytes(bytes(n)),
  utf8 = new TextEncoder(),
  hp = decodeFixed(pin.parameter_base_hash, 32);
const scalar = (label) =>
  1n +
  (BigInt(
    "0x" +
      sha(Buffer.from("CIVORA/v0.7/PUBLIC_TEST_ONLY/" + label)).toString("hex"),
  ) %
    (order - 1n));
const bundle = (files) =>
  Buffer.from(
    canonical({
      suite: "eg-2.0-civic-1",
      files: Object.entries(files)
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([path, content]) => ({
          path,
          bytes: Buffer.from(content).toString("base64url"),
        })),
    }),
  ).toString("base64url");
const original = JSON.parse(
  readFileSync("spec/fixtures/v0.4/actors.TEST_ONLY.json", "utf8"),
).contexts[0].actors.slice(0, 5);
export function actors(network) {
  return original.map((a) => ({
    ...a,
    account_id: domain("account", [network, a.signing.publicKey]),
  }));
}
export async function ceremony(network, poll, proposal, guardians) {
  const ids = guardians.map((g) => g.account_id).sort(),
    coeffs = ids.map((id, i) =>
      [0, 1, 2].map((j) => scalar(`${poll}/${id}/coefficient/${j}`)),
    ),
    key = coeffs.reduce((k, c) => (k * power(generator, c[0])) % prime, 1n),
    manifest = pin.manifest_template.replaceAll("POLL_ID", poll),
    hm = await hash(
      hp,
      1,
      index(Buffer.byteLength(manifest)),
      utf8.encode(manifest),
    ),
    hb = await hash(hp, 2, hm, index(5), index(3)),
    he = await hash(hb, 0x12, bytes(key)),
    files = {
      "manifest.json": manifest,
      "context.json": canonical({
        encoding: "eg-civic-compact-1",
        number_of_guardians: "5",
        quorum: "3",
        parameter_base_hash: pin.parameter_base_hash,
        manifest_hash: encodeBytes(hm),
        election_base_hash: encodeBytes(hb),
        extended_base_hash: encodeBytes(he),
        joint_public_key: group(key),
      }),
      "link.json": canonical({
        network_id: network,
        poll_id: poll,
        proposal_id: proposal,
        question_digest: domain("poll/question", [network, poll, proposal]),
      }),
    };
  for (let i = 0; i < 5; i++) {
    const proofs = [];
    for (let j = 0; j < 3; j++) {
      const secret = coeffs[i][j],
        pk = power(generator, secret),
        w = scalar(`${poll}/${i}/${j}/proof`),
        a = power(generator, w),
        challenge = mod(
          integer(
            await hash(hp, 0x10, index(i + 1), index(j), bytes(pk), bytes(a)),
          ),
        );
      proofs.push({
        public_key: group(pk),
        challenge: enc(challenge),
        response: enc(mod(w - challenge * secret)),
      });
    }
    files[`guardians/0${i + 1}.json`] = canonical({
      account_id: ids[i],
      sequence_order: String(i + 1),
      coefficient_proofs: proofs,
    });
  }
  const encoded = bundle(files),
    context = {
      network_id: network,
      poll_id: poll,
      proposal_id: proposal,
      protocol_epoch: "1",
      guardian_ids: ids,
    };
  await verifyCeremony(context, encoded);
  const byID = new Map(actors(network).map((a) => [a.account_id, a])),
    digest = domain("poll/ceremony", [
      network,
      poll,
      sha(Buffer.from(encoded, "base64url")).toString("hex"),
    ]);
  return {
    encoded,
    coeffs,
    context,
    guardian_acks: ids.map((account_id) => ({
      account_id,
      signature: signDigest(digest, byID.get(account_id).signing.seed),
    })),
  };
}
export async function tally(c, ceremonyBundle, aggregate, count) {
  const e = await verifyCeremony(c.context, ceremonyBundle),
    [a, b] = pair(JSON.parse(aggregate)),
    secret = mod(c.coeffs.reduce((s, v) => s + v[0], 0n)),
    m = power(a, secret),
    bom = (b * power(m, prime - 2n)) % prime;
  let yes = -1n,
    n = BigInt(count),
    running = 1n;
  for (let i = 0n; i <= n; i++) {
    if (running === bom) {
      yes = i;
      break;
    }
    running = (running * e.key) % prime;
  }
  if (yes < 0n) throw Error("development decryption failed");
  const indices = [1n, 3n, 5n],
    shares = indices.map((i) => {
      const shareSecret = mod(
          c.coeffs.reduce(
            (sum, coeff) =>
              sum + coeff.reduce((s, v, j) => s + v * i ** BigInt(j), 0n),
            0n,
          ),
        ),
        u = scalar(`${c.context.poll_id}/tally/${aggregate}/${i}`);
      let w = 1n;
      for (const j of indices)
        if (i !== j) w = mod(w * j * powerQ(mod(j - i), order - 2n));
      return {
        i,
        w,
        secret: shareSecret,
        u,
        share: power(a, shareSecret),
        a: power(generator, u),
        b: power(a, u),
      };
    }),
    ga = shares.reduce((s, v) => (s * v.a) % prime, 1n),
    gb = shares.reduce((s, v) => (s * v.b) % prime, 1n),
    challenge = mod(
      integer(
        await hash(
          e.he,
          0x30,
          bytes(e.key),
          bytes(a),
          bytes(b),
          bytes(ga),
          bytes(gb),
          bytes(m),
        ),
      ),
    ),
    records = shares.map((s) => ({
      guardian_index: String(s.i),
      share: group(s.share),
      a: group(s.a),
      b: group(s.b),
      response: enc(mod(s.u - challenge * s.w * s.secret)),
    })),
    response = mod(
      records.reduce((s, r) => s + integer(decodeFixed(r.response, 32)), 0n),
    ),
    total = {
      encoding: "eg-civic-compact-1",
      id: c.context.poll_id,
      election_id: encodeBytes(e.he),
      ciphertext: { alpha: group(a), beta: group(b) },
      tally: String(yes),
      b_over_m: group(bom),
      proof: { challenge: enc(challenge), response: enc(response) },
      shares: records,
    },
    encoded = bundle({ "tally.json": canonical(total) });
  await verifyTally(e, aggregate, count, encoded);
  return { yes: String(yes), no: String(n - yes), decryption_record: encoded };
}
function powerQ(a, b) {
  let v = 1n;
  for (; b > 0n; b >>= 1n, a = (a * a) % order) if (b & 1n) v = (v * a) % order;
  return v;
}
