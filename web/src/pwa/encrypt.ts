// Development client encryption for the pinned Civic compact mapping. BigInt
// timing/erasure hardening is not a public security qualification (v0.9).
import { canonical } from "../codec.js";
import {
  bytes,
  concat,
  index,
  integer,
  mod,
  power,
  prime,
  order,
  generator,
  hash,
  encodeBytes,
  verifyBallot,
  type PublicElection,
} from "../electionguard.js";
const utf8 = new TextEncoder();
export function randomScalar(): bigint {
  for (;;) {
    const raw = crypto.getRandomValues(new Uint8Array(32)),
      n = integer(raw);
    raw.fill(0);
    if (n > 0n && n < order) return n;
  }
}
// Random exponent blinding avoids direct reuse of a secret exponent trace.
// JS BigInt still cannot provide a constant-time or secure-erasure promise.
function secretPower(base: bigint, exponent: bigint): bigint {
  return power(
    base,
    exponent + order * integer(crypto.getRandomValues(new Uint8Array(32))),
  );
}
async function range(
  e: PublicElection,
  a: bigint,
  b: bigint,
  nonce: bigint,
  choice: 0 | 1,
) {
  const fake = 1 - choice,
    cFake = randomScalar(),
    rFake = randomScalar(),
    w = randomScalar(),
    pairs: Uint8Array[] = [];
  for (let j = 0; j < 2; j++) {
    if (j === choice)
      pairs.push(
        bytes(secretPower(generator, w)),
        bytes(secretPower(e.key, w)),
      );
    else
      pairs.push(
        bytes((secretPower(generator, rFake) * power(a, cFake)) % prime),
        bytes(
          (secretPower(e.key, mod(rFake - BigInt(j) * cFake)) *
            power(b, cFake)) %
            prime,
        ),
      );
  }
  const challenge = mod(
      integer(
        await hash(e.he, 0x21, bytes(e.key), bytes(a), bytes(b), ...pairs),
      ),
    ),
    cTrue = mod(challenge - cFake),
    rTrue = mod(w - cTrue * nonce);
  return [0, 1].map((j) => ({
    challenge: encodeBytes(bytes(j === fake ? cFake : cTrue, 32)),
    response: encodeBytes(bytes(j === fake ? rFake : rTrue, 32)),
  }));
}
async function hmac(key: Uint8Array, data: Uint8Array): Promise<Uint8Array> {
  const k = await crypto.subtle.importKey(
    "raw",
    new Uint8Array(key).buffer,
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  return new Uint8Array(
    await crypto.subtle.sign("HMAC", k, new Uint8Array(data).buffer),
  );
}
async function contestData(e: PublicElection, choice: 0 | 1) {
  const nonce = randomScalar(),
    alpha = secretPower(generator, nonce),
    beta = secretPower(e.key, nonce),
    key = await hash(e.he, 0x22, bytes(e.key), bytes(alpha), bytes(beta)),
    kdf = (i: number) =>
      hmac(
        key,
        concat(
          index(i),
          utf8.encode("share_enc_keys"),
          Uint8Array.of(0),
          utf8.encode("contest_datacivic_question"),
          index(62 * 8),
        ),
      ),
    plain = new Uint8Array(64),
    status = utf8.encode(choice === 0 ? "under_vote" : "normal");
  plain.set([0, 0, status.length]);
  plain.set(status, 3);
  const c1 = concat(await kdf(1), await kdf(2));
  for (let i = 0; i < 64; i++) c1[i] = c1[i]! ^ plain[i]!;
  plain.fill(0);
  const c2 = await hmac(await kdf(0), concat(bytes(alpha), c1));
  key.fill(0);
  return {
    c0: encodeBytes(bytes(alpha)),
    c1: encodeBytes(c1),
    c2: encodeBytes(c2),
    num_bytes: "62",
  };
}
export async function encryptVote(
  e: PublicElection,
  choice: 0 | 1,
): Promise<{
  encoded: string;
  nonce: Uint8Array;
  alpha: string;
  raw: Uint8Array;
}> {
  if (choice !== 0 && choice !== 1) throw Error("ERR_CHOICE");
  const nonce = randomScalar(),
    a = secretPower(generator, nonce),
    b = secretPower(e.key, nonce + BigInt(choice)),
    ch = await hash(e.he, 0x23, index(0), bytes(e.key), bytes(a), bytes(b)),
    ballot = {
      encoding: "eg-civic-compact-1",
      ballot_id: encodeBytes(crypto.getRandomValues(new Uint8Array(32))),
      ballot_style_id: "civic",
      encrypting_device: "civic:" + e.context.poll_id,
      timestamp: "0",
      code_baux: "",
      confirmation_code: encodeBytes(await hash(e.he, 0x24, ch)),
      election_id: encodeBytes(e.he),
      state: "CAST",
      contests: [
        {
          contest_id: "civic_question",
          sequence_order: "0",
          votes_allowed: "1",
          contest_hash: encodeBytes(ch),
          selections: [
            {
              selection_id: "yes_bit",
              sequence_order: "0",
              encrypted_vote: {
                alpha: encodeBytes(bytes(a)),
                beta: encodeBytes(bytes(b)),
              },
              proof: await range(e, a, b, nonce, choice),
            },
          ],
          proof: await range(e, a, b, nonce, choice),
          encrypted_contest_data: await contestData(e, choice),
        },
      ],
    };
  ballot.ballot_id = Array.from(
    crypto.getRandomValues(new Uint8Array(32)),
    (b) => b.toString(16).padStart(2, "0"),
  ).join("");
  const raw = utf8.encode(canonical(ballot)),
    encoded = encodeBytes(
      utf8.encode(
        canonical({
          suite: "eg-2.0-civic-1",
          files: [{ path: "ballot.json", bytes: encodeBytes(raw) }],
        }),
      ),
    );
  await verifyBallot(e, encoded);
  return {
    encoded,
    nonce: bytes(nonce, 32),
    alpha: encodeBytes(bytes(a)),
    raw,
  };
}
