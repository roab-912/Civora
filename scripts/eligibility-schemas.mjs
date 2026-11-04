export function eligibilitySchemas(
  d,
  { object, list, ref, id, uint, constant },
) {
  const field = { type: "string", pattern: "^(0|[1-9][0-9]{0,76})$" };
  const nullable = (x) => ({ anyOf: [x, { type: "null" }] });
  d.eligibility_config = object({
    circuit_depth: { enum: ["16", "26"] },
    suite_id: {
      type: "string",
      pattern: "^semaphore-v4\\.14\\.5-depth-(16|26)$",
    },
  });
  d.config.properties.eligibility = ref("eligibility_config");
  d.account.properties.eligibility_index = uint;
  d.eligibility_snapshot = object({
    network_id: id,
    protocol_epoch: uint,
    tree_id: { type: "string", minLength: 1, maxLength: 96 },
    suite_id: d.eligibility_config.properties.suite_id,
    height: uint,
    circuit_depth: { enum: ["16", "26"] },
    size: uint,
    active_count: uint,
    root: nullable(field),
    snapshot_id: id,
  });
  d.eligibility_path = object({
    snapshot: ref("eligibility_snapshot"),
    leaf_index: uint,
    leaf: field,
    index: uint,
    siblings: list(field, 26),
  });
  d.membership_v03 = object({
    merkleTreeDepth: { enum: ["16", "26"] },
    merkleTreeRoot: field,
    nullifier: field,
    message: field,
    scope: field,
    points: {
      ...list({ type: "string", pattern: "^(0|[1-9][0-9]{0,76})$" }, 8),
      minItems: 8,
    },
  });
  d.membership_context = object({
    network_id: id,
    poll_id: id,
    protocol_epoch: uint,
    suite_id: d.eligibility_config.properties.suite_id,
    circuit_depth: { enum: ["16", "26"] },
    group_root: field,
  });
  d.entry.oneOf[1].properties.value.properties.eligibility = ref(
    "eligibility_snapshot",
  );
  d.entry.oneOf.push(
    object({ table: constant("eligibility"), key: uint, value: field }),
  );
}
