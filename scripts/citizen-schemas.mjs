export function citizenSchemas(d, { object, list, ref, id, uint, constant }) {
  const text = (max) => ({ type: "string", minLength: 1, maxLength: max });
  const blob = (max) => ({
    type: "string",
    pattern: "^[A-Za-z0-9_-]+$",
    maxLength: max,
  });
  d.archive_ref = object({ sha256: id, size: uint });
  d.citizen_archive_request = object({
    poll_id: id,
    lane_id: uint,
    start: uint,
    envelopes: { ...list(blob(349526), 32), minItems: 1 },
  });
  d.citizen_relay_job = object({
    chain_id: text(50),
    body: blob(349526),
    kind: { enum: ["ballot", "transaction"] },
  });
  d.archive_block = object({
    block: blob(5592406),
    signed_header: blob(65536),
  });
  d.citizen_origin = object({ api: text(2048), rpc: text(2048) });
  d.citizen_chain = object({
    chain_id: text(50),
    lane_id: { oneOf: [uint, { type: "null" }] },
    genesis: ref("archive_ref"),
    origins: { ...list(ref("citizen_origin"), 16), minItems: 2 },
    peers: { ...list(text(300), 32), minItems: 1 },
  });
  d.citizen_manifest = object({
    format: constant("civic-network-1"),
    name: text(80),
    mode: constant("development"),
    network_id: id,
    voting_profile: {
      enum: ["inline_dev", "transport_dev_v0_5", "transport_dev_v0_6"],
    },
    main: ref("citizen_chain"),
    lanes: list(ref("citizen_chain"), 256),
  });
  d.state_witness = object({
    entry: object({ table: text(96), key: {}, value: {} }),
    exists: { type: "boolean" },
    siblings: { ...list(id, 256), minItems: 256 },
  });
  d.citizen_state_proof = object({
    height: uint,
    signed_header: blob(65536),
    path: ref("state_witness"),
  });
  d.citizen_error = object({
    code: text(64),
    message: text(160),
    retryable: { type: "boolean" },
  });
  d.citizen_cursor = object({
    height: uint,
    last: { type: "string", maxLength: 256 },
    resource: text(64),
    filter: { type: "string", maxLength: 256 },
  });
  d.citizen_listener = object({ url: text(2048) });
  d.audit_config = object({
    keys: { ...list(id, 5), minItems: 5, uniqueItems: true },
    implementations: {
      type: "array",
      prefixItems: [
        constant("civic-go-v0.6"),
        constant("civic-typescript-v0.6"),
      ],
      minItems: 2,
      maxItems: 2,
      items: false,
    },
  });
  d.audit_attestation = object({
    poll_id: id,
    dossier_id: id,
    implementation: { enum: ["civic-go-v0.6", "civic-typescript-v0.6"] },
    signer: id,
    signature: { type: "string", pattern: "^[0-9a-f]{128}$" },
  });
  for (const name of ["coordinator_config", "partition_frozen"]) {
    d[name].properties.audit = ref("audit_config");
  }
  d.coordinator_config.properties.profile = {
    enum: ["transport_dev_v0_5", "transport_dev_v0_6"],
  };
  d.coordinator_tx.oneOf.push(
    object({ type: constant("audit.attest"), value: ref("audit_attestation") }),
    object({
      type: constant("audit.challenge"),
      value: ref("audit_challenge"),
    }),
  );
  d.audit_evidence = object({
    lane_id: uint,
    index: uint,
    envelope: blob(349526),
    admission_path: list(id, 64),
    status: ref("state_witness"),
  });
  d.audit_challenge = object({
    poll_id: id,
    kind: {
      enum: [
        "counted_invalid",
        "wrong_route",
        "double_counted",
        "valid_omitted",
      ],
    },
    items: { ...list(ref("audit_evidence"), 2), minItems: 1 },
  });
  d.partition_provisional = object({
    aggregate: { type: "string", maxLength: 2048 },
    count: uint,
    yes: uint,
    no: uint,
    state: {
      enum: ["provisional", "certified", "contested", "failed_certification"],
    },
    decryption_record: {
      type: "string",
      pattern: "^[A-Za-z0-9_-]*$",
      maxLength: 174763,
    },
    dossier_id: id,
    published_ms: uint,
    published_height: uint,
  });
  d.partition_provisional.required = d.partition_provisional.required.filter(
    (k) => !["dossier_id", "published_ms", "published_height"].includes(k),
  );
  d.audit_certification = object({
    poll_id: id,
    dossier_id: id,
    published_ms: uint,
    certified_height: uint,
    certified_ms: uint,
    attestations: { ...list(ref("audit_attestation"), 5), minItems: 3 },
  });
  d.election_batch = object({
    reference: ref("partition_batch_ref"),
    envelopes: { ...list(ref("archive_ref"), 32), minItems: 1 },
    signed_header: blob(65536),
    transaction_proof: blob(546134),
  });
  d.election_lane = object({
    lane_id: uint,
    accepted: ref("citizen_state_proof"),
    checkpoints: { ...list(ref("partition_checkpoint"), 4096), minItems: 1 },
    batches: list(ref("election_batch"), 65536),
  });
  d.citizen_election_record = object({
    format: constant("civic-election-1"),
    genesis: ref("archive_ref"),
    frozen: ref("partition_frozen"),
    opening: ref("consensus_state_proof"),
    lanes: { ...list(ref("election_lane"), 256), minItems: 1 },
    result: ref("citizen_state_proof"),
    status: ref("citizen_state_proof"),
    certificate: ref("citizen_state_proof"),
  });
  d.citizen_election_record.required =
    d.citizen_election_record.required.filter((k) => k !== "certificate");
  d.citizen_record_index = object({
    format: constant("civic-record-index-1"),
    network_id: id,
    poll_id: id,
    dossier_id: id,
    record: ref("archive_ref"),
    objects: list(ref("archive_ref"), 65536),
  });
  d.citizen_test_key = object({
    label: constant("PUBLIC_TEST_ONLY"),
    seed: id,
    public_key: id,
  });
}
