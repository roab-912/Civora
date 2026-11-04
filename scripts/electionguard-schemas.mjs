// Civic envelopes only. External ElectionGuard schemas remain unqualified.
export function electionguardSchemas(
  d,
  { object, list, id, constant, ref, uint },
) {
  d.eg_bundle = object({
    suite: constant("eg-2.0-civic-1"),
    files: {
      ...list(
        object({
          path: {
            enum: [
              "context.json",
              "guardians/01.json",
              "guardians/02.json",
              "guardians/03.json",
              "guardians/04.json",
              "guardians/05.json",
              "link.json",
              "manifest.json",
              "ballot.json",
              "tally.json",
            ],
          },
          bytes: {
            type: "string",
            minLength: 1,
            maxLength: 87382,
            pattern: "^[A-Za-z0-9_-]+$",
          },
        }),
        8,
      ),
      minItems: 1,
    },
  });
  d.eg_link = object({
    network_id: id,
    poll_id: id,
    proposal_id: id,
    question_digest: id,
  });
  const blob = (length) => ({
    type: "string",
    pattern: `^[A-Za-z0-9_-]{${length}}$`,
  });
  const p = blob(683),
    q = blob(43),
    encoding = constant("eg-civic-compact-1");
  const fixed = (items, count) => ({ ...list(items, count), minItems: count });
  d.eg_proof = object({ challenge: q, response: q });
  d.eg_ciphertext = object({ alpha: p, beta: p });
  d.eg_context = object({
    encoding,
    number_of_guardians: constant("5"),
    quorum: constant("3"),
    parameter_base_hash: q,
    manifest_hash: q,
    election_base_hash: q,
    extended_base_hash: q,
    joint_public_key: p,
  });
  d.eg_guardian = object({
    account_id: id,
    sequence_order: { enum: ["1", "2", "3", "4", "5"] },
    coefficient_proofs: fixed(
      object({ public_key: p, challenge: q, response: q }),
      3,
    ),
  });
  const range = fixed(ref("eg_proof"), 2);
  d.eg_ballot = object({
    encoding,
    ballot_id: id,
    ballot_style_id: constant("civic"),
    encrypting_device: { type: "string", pattern: "^civic:[0-9a-f]{64}$" },
    timestamp: constant("0"),
    code_baux: constant(""),
    confirmation_code: q,
    election_id: q,
    state: constant("CAST"),
    contests: fixed(
      object({
        contest_id: constant("civic_question"),
        sequence_order: constant("0"),
        votes_allowed: constant("1"),
        contest_hash: q,
        selections: fixed(
          object({
            selection_id: constant("yes_bit"),
            sequence_order: constant("0"),
            encrypted_vote: ref("eg_ciphertext"),
            proof: range,
          }),
          1,
        ),
        proof: range,
        encrypted_contest_data: object({
          c0: p,
          c1: { type: "string", pattern: "^[A-Za-z0-9_-]+$", maxLength: 2048 },
          c2: q,
          num_bytes: uint,
        }),
      }),
      1,
    ),
  });
  d.eg_tally = object({
    encoding,
    id,
    election_id: q,
    ciphertext: ref("eg_ciphertext"),
    tally: uint,
    b_over_m: p,
    proof: ref("eg_proof"),
    shares: {
      ...list(
        object({
          guardian_index: { enum: ["1", "2", "3", "4", "5"] },
          share: p,
          a: p,
          b: p,
          response: q,
        }),
        5,
      ),
      minItems: 3,
    },
  });
  d.eg_independence = object({ commitment: p, response: q });
  d.scale_context = object({
    network_id: id,
    poll_id: id,
    protocol_epoch: uint,
    group_root: { type: "string", pattern: "^(0|[1-9][0-9]{0,76})$" },
    circuit_depth: { enum: ["16", "26"] },
    partition_count: uint,
    checkpoint_id: id,
  });
  d.scale_envelope = object({
    network_id: id,
    poll_id: id,
    protocol_epoch: uint,
    lane_id: uint,
    checkpoint_id: id,
    encrypted_ballot: {
      type: "string",
      pattern: "^[A-Za-z0-9_-]+$",
      maxLength: 174763,
    },
    independence: ref("eg_independence"),
    membership: ref("membership_v03"),
  });
  d.governance_config.properties.crypto_profile.enum.push("electionguard_v0_4");
  d.eg_archive = object({
    format: constant("civic-audit-1"),
    genesis: ref("config"),
    records: list(ref("commit"), 8192),
  });
}
