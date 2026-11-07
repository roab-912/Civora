export function recoverySchemas(d, { object, list, ref, id, uint, constant }) {
  const digest = { type: "string", pattern: "^[0-9a-f]{96}$" },
    name = { type: "string", pattern: "^[a-z][a-z0-9._/-]{0,95}$" },
    key = {
      type: "string",
      pattern: "^[A-Za-z0-9_-]+$",
      minLength: 2603,
      maxLength: 2603,
    },
    signature = {
      type: "string",
      pattern: "^[A-Za-z0-9_-]+$",
      minLength: 4412,
      maxLength: 4412,
    },
    signatures = list(object({ member_id: name, signature }), 14);
  d.recovery_root = object({
    v: constant("1"),
    network_id: id,
    recovery_epoch: uint,
    mode: { enum: ["development_TEST_ONLY", "operator"] },
    not_before_ms: uint,
    expires_at_ms: uint,
    members: {
      ...list(
        object({
          member_id: name,
          college: { enum: ["technical", "civic"] },
          public_key: key,
        }),
        14,
      ),
      minItems: 14,
    },
    mirrors: {
      ...list(object({ member_id: name, public_key: key }), 5),
      minItems: 5,
    },
  });
  d.recovery_order = object({
    v: constant("1"),
    network_id: id,
    recovery_epoch: uint,
    counter: uint,
    type: {
      enum: ["security.hold", "security.arm_reserve", "recovery.publish"],
    },
    scope_kind: { enum: ["module", "profile", "poll", "effect", "network"] },
    scope_id: { type: "string", minLength: 1, maxLength: 96 },
    reference_sha3_384: digest,
    reference_point: { type: "string", minLength: 1, maxLength: 4096 },
    public_reason: { type: "string", minLength: 1, maxLength: 4096 },
    starts_at_ms: uint,
    expires_at_ms: uint,
  });
  d.recovery_mirror_statement = object({
    network_id: id,
    recovery_epoch: uint,
    counter: uint,
    root_sha3_384: digest,
    order_sha3_384: digest,
    observed_at_ms: uint,
    expires_at_ms: uint,
  });
  d.recovery_bundle = object({
    order: ref("recovery_order"),
    signatures,
    mirrors: list(
      object({
        member_id: name,
        statement: ref("recovery_mirror_statement"),
        signature,
      }),
      5,
    ),
  });
  d.recovery_rotation = object({
    next_root: ref("recovery_root"),
    approval_sha3_384: digest,
    old_signatures: signatures,
    new_signatures: signatures,
  });
  d.recovery_config = object({
    root: ref("recovery_root"),
    root_sha3_384: digest,
    protected_profile: {
      enum: ["inline_dev", "transport_dev_v0_5", "transport_dev_v0_6"],
    },
  });
  d.security_hold_tx = object({
    type: constant("security.hold"),
    value: ref("recovery_bundle"),
  });
  d.config.properties.recovery = ref("recovery_config");
  d.block.properties.security_orders = list(ref("recovery_bundle"), 4);
  d.partition_config.properties.recovery = ref("recovery_config");
  d.partition_frozen.properties.recovery = ref("recovery_config");
  d.partition_summary.properties.interrupted = { const: true };
  d.partition_provisional.properties.state.enum.push("interrupted");
  d.entry.oneOf.push(
    object({
      table: constant("security"),
      key: constant("recovery"),
      value: object({ last_counter: uint }),
    }),
    object({
      table: constant("security_orders"),
      key: uint,
      value: ref("recovery_bundle"),
    }),
  );
  d.event.properties.type.enum.push("security.hold");
  // Governance extends this enum through the shared status object.
  d.poll.properties.state.enum.push("interrupted");
  d.event.properties.type.enum.push("proposal.interrupted");
}
