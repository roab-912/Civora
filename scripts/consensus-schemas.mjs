export function consensusSchemas(d, { object, list, ref, id, uint, constant }) {
  const blob = {
    type: "string",
    pattern: "^[A-Za-z0-9_-]*$",
    maxLength: 349526,
  };
  d.replica_block = object({
    height: uint,
    time_ms: uint,
    transactions: list(blob, 8192),
  });
  d.replica_record = object({
    block: ref("replica_block"),
    identity: object({
      hash: id,
      time: { type: "string", minLength: 20, maxLength: 40 },
    }),
    state: {},
    app_hash: id,
  });
  d.network_genesis = {
    oneOf: [
      ...["ledger", "partition", "coordinator"].map((kind) => {
        const s = object({
          kind: constant(kind),
          data: ref(
            kind === "ledger"
              ? "config"
              : kind === "partition"
                ? "partition_config"
                : "coordinator_config",
          ),
          consensus_rotations: list(ref("consensus_rotation"), 64),
        });
        s.required = ["kind", "data"];
        return s;
      }),
    ],
  };
  d.network_settings = object({
    label: constant("PUBLIC_TEST_KEYS_ONLY"),
    chain_id: { type: "string", minLength: 1, maxLength: 50 },
    genesis_sha256: id,
    seat: { type: "string", pattern: "^[0-6]$" },
    port: uint,
    peers: list({ type: "string", minLength: 1, maxLength: 128 }, 6),
  });
  const chain = { type: "string", minLength: 1, maxLength: 50 },
    encoded = (max) => ({
      type: "string",
      pattern: "^[A-Za-z0-9_-]+$",
      maxLength: max,
    });
  d.consensus_trust = object({
    chain_id: chain,
    keys: { ...list(id, 7), minItems: 7, uniqueItems: true },
  });
  d.state_membership = object({
    entry: {
      oneOf: [
        ref("entry"),
        object({
          table: constant("transport_polls"),
          key: id,
          value: ref("partition_frozen"),
        }),
        object({
          table: constant("lane_checkpoint"),
          key: id,
          value: ref("partition_summary"),
        }),
      ],
    },
    siblings: { ...list(id, 256), minItems: 256 },
  });
  d.consensus_state_proof = object({
    height: uint,
    signed_header: encoded(65536),
    path: ref("state_membership"),
  });
  d.partition_frozen = object({
    context: ref("scale_context"),
    proposal_id: id,
    guardian_ids: { ...list(id, 5), minItems: 5, uniqueItems: true },
    ceremony_bundle: encoded(174763),
    opens_at_ms: uint,
    closes_at_ms: uint,
    source_height: uint,
    lanes: { ...list(ref("consensus_trust"), 256), minItems: 1 },
    archive_keys: { ...list(id, 5), minItems: 5, uniqueItems: true },
    eligible_count: uint,
  });
  d.partition_config = object({
    network_id: id,
    chain_id: chain,
    lane_id: uint,
    protocol_epoch: uint,
    source: ref("consensus_trust"),
  });
  d.archive_signature = object({
    key: id,
    signature: { type: "string", pattern: "^[0-9a-f]{128}$" },
  });
  d.partition_batch = object({
    poll_id: id,
    envelopes: { ...list(encoded(349526), 32), minItems: 1 },
    certificate: { ...list(ref("archive_signature"), 5), minItems: 3 },
  });
  d.partition_process = object({
    poll_id: id,
    count: { enum: ["1", "2", "3", "4"] },
  });
  d.partition_tx = {
    oneOf: [
      object({
        type: constant("partition.open"),
        value: ref("consensus_state_proof"),
      }),
      object({
        type: constant("partition.admit"),
        value: ref("partition_batch"),
      }),
      object({
        type: constant("partition.process"),
        value: ref("partition_process"),
      }),
    ],
  };
  d.partition_batch_ref = object({
    id,
    network_id: id,
    poll_id: id,
    lane_id: uint,
    protocol_epoch: uint,
    start: uint,
    count: uint,
    root: id,
    manifest_sha256: id,
    retention: constant("persisted-development-v0.5"),
  });
  d.partition_summary = object({
    network_id: id,
    poll_id: id,
    lane_id: uint,
    protocol_epoch: uint,
    context_id: id,
    height: uint,
    time_ms: uint,
    admitted_count: uint,
    admission_root: id,
    processed_count: uint,
    status_root: id,
    counted_count: uint,
    spent_root: id,
    aggregate: { type: "string", maxLength: 2048 },
    batch_count: uint,
    batch_root: id,
    admission_sealed: { type: "boolean" },
    processing_sealed: { type: "boolean" },
  });
  d.partition_batch_proof = object({
    reference: ref("partition_batch_ref"),
    certificate: { ...list(ref("archive_signature"), 5), minItems: 3 },
    inclusion: list(id, 64),
  });
  d.partition_checkpoint = object({
    proof: ref("consensus_state_proof"),
    previous_id: id,
    admission_consistency: list(id, 64),
    batch_consistency: list(id, 64),
    extension: list(ref("partition_batch_proof"), 64),
  });
  d.coordinator_config = object({
    ledger: ref("config"),
    lanes: { ...list(ref("consensus_trust"), 256), minItems: 1 },
    archive_keys: { ...list(id, 5), minItems: 5, uniqueItems: true },
    circuit_depth: constant("26"),
    profile: constant("transport_dev_v0_5"),
  });
  d.partition_tally = object({
    poll_id: id,
    decryption_record: { oneOf: [constant(""), encoded(174763)] },
  });
  d.coordinator_tx = {
    oneOf: [
      object({
        type: constant("lane.checkpoint"),
        value: ref("partition_checkpoint"),
      }),
      object({
        type: constant("partition.tally"),
        value: ref("partition_tally"),
      }),
    ],
  };
  d.partition_receipt = object({
    signed_header: encoded(65536),
    transaction_proof: encoded(546134),
    reference: ref("partition_batch_ref"),
    envelope: encoded(349526),
    index: uint,
    inclusion: list(id, 64),
  });
  d.consensus_rotation = object({
    height: uint,
    keys: { ...list(id, 7), minItems: 7, uniqueItems: true },
    guardians: {
      oneOf: [
        { ...list(object({ account_id: id, signing_pk: id }), 5), minItems: 5 },
        { type: "array", maxItems: 0 },
      ],
    },
    authorization: constant("development_genesis_fixture"),
  });
}
