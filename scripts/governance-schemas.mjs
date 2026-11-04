// v0.2 extends the development schema while preserving all v0.1 documents.
export function governanceSchemas(
  d,
  { object, list, ref, id, uint, constant },
) {
  const nullable = (schema) => ({ anyOf: [schema, { type: "null" }] });
  const text = (max) => ({ type: "string", maxLength: max });
  const blob = {
    type: "string",
    pattern: "^[A-Za-z0-9_-]*$",
    maxLength: 349526,
  };
  const field = { type: "string", pattern: "^(0|[1-9][0-9]{0,77})$" };
  const caps = {
    enum: ["annotate_proposal", "publish_notice", "report_implementation"],
  };
  d.policy = object(
    Object.fromEntries(
      [
        "collection_ms",
        "queue_ms",
        "setup_ms",
        "notice_ms",
        "voting_ms",
        "tally_ms",
        "endorsements_required",
        "ordinary_quorum_bps",
        "constitutional_quorum_bps",
        "ordinary_execution_delay_ms",
        "constitutional_execution_delay_ms",
      ].map((k) => [k, uint]),
    ),
  );
  d.guardian = object({ account_id: id, signing_pk: id });
  const guardians = { ...list(ref("guardian"), 5), minItems: 5 };
  d.governance_config = object({
    policy: ref("policy"),
    guardians,
    crypto_profile: { enum: ["unconfigured", "simulation_v0_2"] },
    eligibility_suite: d.module.properties.id,
    ballot_suite: d.module.properties.id,
    tally_suite: d.module.properties.id,
    effect_profile: constant("ledger_only_v1"),
  });
  d.governance_network = object({
    policy: ref("policy"),
    policy_revision: uint,
    guardian_epoch: uint,
    guardians,
  });
  d.config.properties.governance = ref("governance_config");
  d.entry.oneOf[1].properties.value.properties.governance =
    ref("governance_network");
  d.law_content = object({
    title: text(160),
    text: text(131072),
    language: {
      type: "string",
      minLength: 1,
      maxLength: 255,
      pattern: "^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*$",
    },
  });
  d.role_content = object({
    label: text(160),
    description: text(8192),
    capabilities: { ...list(caps, 5), uniqueItems: true },
    scope_id: nullable(id),
  });
  const actionPayloads = {
    "law.create": ref("law_content"),
    "law.amend": ref("law_content"),
    "law.repeal": object({}),
    "law.restore": object({ source_revision: uint }),
    "role.create": ref("role_content"),
    "role.amend": ref("role_content"),
    "role.retire": object({}),
    "role.restore": object({ source_revision: uint }),
    "role.grant": object({ account_id: id, until_ms: uint }),
    "role.revoke": object({ grant_id: id }),
    "policy.update": object({ policy: ref("policy") }),
  };
  const submissions = Object.entries(actionPayloads).map(([action, changes]) =>
    object({
      action: constant(action),
      target_id:
        action.endsWith(".create") || action === "policy.update"
          ? { type: "null" }
          : id,
      expected_revision: action.endsWith(".create") ? { type: "null" } : uint,
      rationale: text(16384),
      requested_effect_ms: nullable(uint),
      changes,
    }),
  );
  d.submission = { oneOf: submissions };
  d.publication = object({
    role_id: id,
    capability: caps,
    target_id: id,
    content: text(8192),
  });
  d.membership = object({
    merkleTreeDepth: constant("16"),
    merkleTreeRoot: field,
    nullifier: field,
    message: field,
    scope: field,
    points: { ...list(field, 8), minItems: 8 },
  });
  const signature = { type: "string", pattern: "^[0-9a-f]{128}$" };
  d.configure = object({
    poll_id: id,
    ceremony_bundle: blob,
    guardian_acks: {
      ...list(object({ account_id: id, signature }), 5),
      minItems: 5,
    },
  });
  d.vote = object({
    poll_id: id,
    encrypted_ballot: blob,
    membership: ref("membership"),
  });
  d.tally = object({
    poll_id: id,
    yes: uint,
    no: uint,
    aggregate_digest: id,
    decryption_record: blob,
  });
  const payloads = {
    "proposal.submit": ref("submission"),
    "proposal.endorse": object({ proposal_id: id }),
    "role.publish": ref("publication"),
    "poll.configure": ref("configure"),
    "vote.cast": ref("vote"),
    "poll.tally": ref("tally"),
  };
  for (const version of ["1", "2"])
    for (const [type, payload] of Object.entries(payloads)) {
      const proof = ["poll.configure", "vote.cast", "poll.tally"].includes(
        type,
      );
      const body = {
        v: constant(version),
        network_id: id,
        type: constant(type),
        actor_id: proof ? { type: "null" } : id,
        nonce: proof ? { type: "null" } : uint,
        payload,
      };
      if (version === "2") body.protocol_epoch = uint;
      d.transaction.oneOf.push(
        object({
          body: object(body),
          auth: proof
            ? object({ scheme: constant("proof") })
            : object({ scheme: constant("ed25519"), signature }),
        }),
      );
    }
  const status = {
    enum: [
      "collecting",
      "qualified",
      "preparing",
      "scheduled",
      "open",
      "tallying",
      "adopted",
      "applied",
      "rejected",
      "conflict",
      "expired",
      "failed_setup",
      "failed_window",
      "failed_tally",
    ],
  };
  const proposalFields = {
    author: id,
    state: status,
    policy: ref("policy"),
    policy_revision: uint,
    guardians,
    guardian_epoch: uint,
    endorsements: { ...list(id, 65536), uniqueItems: true },
    submitted_height: uint,
    submitted_ms: uint,
    collection_due_ms: uint,
    qualified_height: nullable(uint),
    qualified_ms: nullable(uint),
    queue_due_ms: nullable(uint),
    setup_due_ms: nullable(uint),
    poll_id: nullable(id),
    effective_at_ms: nullable(uint),
    reason: nullable(text(4096)),
    effect_receipt: nullable(
      object({ profile: constant("ledger_only_v1"), reference: id }),
    ),
  };
  d.proposal = {
    oneOf: submissions.map((s) =>
      object({ ...s.properties, ...proposalFields }),
    ),
  };
  d.poll = object({
    proposal_id: id,
    state: status,
    protocol_epoch: uint,
    eligibility_suite: d.module.properties.id,
    ballot_suite: d.module.properties.id,
    tally_suite: d.module.properties.id,
    opens_at_ms: nullable(uint),
    closes_at_ms: nullable(uint),
    tally_due_at_ms: nullable(uint),
    group_root: nullable(field),
    eligible_count: uint,
    snapshot_height: nullable(uint),
    commitments: list(d.payload.properties.commitment, 65536),
    aggregate: text(349526),
    count: uint,
    closed_height: nullable(uint),
    aggregate_digest: nullable(id),
    ceremony_bundle: nullable(blob),
    ceremony_digest: nullable(id),
    result: nullable(
      object({
        yes: uint,
        no: uint,
        empty: { type: "boolean" },
        certified_height: uint,
        certified_ms: uint,
      }),
    ),
  });
  d.object = object({
    kind: { enum: ["law", "role"] },
    revision: uint,
    status: { enum: ["active", "repealed", "retired"] },
  });
  d.revision = object({
    content: { oneOf: [ref("law_content"), ref("role_content")] },
    proposal_id: id,
    source_revision: nullable(uint),
    status: { enum: ["active", "repealed", "retired"] },
    effective_at_ms: uint,
  });
  d.grant = object({
    account_id: id,
    role_id: id,
    from_ms: uint,
    until_ms: uint,
    status: { enum: ["active", "revoked", "expired"] },
    revoked_by: nullable(id),
    ended_at_ms: nullable(uint),
  });
  const keyTuple = (items) => ({
    type: "array",
    prefixItems: items,
    items: false,
    minItems: items.length,
    maxItems: items.length,
  });
  const addEntry = (table, key, value) =>
    d.entry.oneOf.push(object({ table: constant(table), key, value }));
  addEntry("proposals", id, ref("proposal"));
  addEntry("polls", id, ref("poll"));
  addEntry("objects", keyTuple([{ enum: ["law", "role"] }, id]), ref("object"));
  addEntry(
    "revisions",
    keyTuple([{ enum: ["law", "role"] }, id, uint]),
    ref("revision"),
  );
  addEntry("grants", keyTuple([id, id]), ref("grant"));
  addEntry(
    "spent",
    keyTuple([id, field]),
    object({ tx_id: id, height: uint, position: uint }),
  );
  addEntry(
    "ballots",
    keyTuple([id, id]),
    object({ digest: id, confirmation_code: id, height: uint, tx_id: id }),
  );
  addEntry("ciphertexts", keyTuple([id, id]), id);
  d.event.properties.type.enum.push(
    ...Object.keys(payloads),
    ...status.enum.map((s) => "proposal." + s),
    "grant.expired",
    "grant.revoked",
    "poll.empty",
  );
  d.event.properties.type.enum = [...new Set(d.event.properties.type.enum)];
  d.state.properties.entries.maxItems = 1048576;
  d.commit.properties.events.maxItems = 32768;
  d.fixture.properties.expected_state = ref("state");
}
