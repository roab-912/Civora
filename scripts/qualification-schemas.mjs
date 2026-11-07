export function qualificationSchemas(
  d,
  { object, list, ref, id, uint, constant },
) {
  const artifact = object({
    path: { type: "string", minLength: 1, maxLength: 1024 },
    sha256: id,
    size: uint,
  });
  const optionalArtifact = { anyOf: [artifact, { type: "null" }] };
  d.qualification_policy = object({
    v: constant("1"),
    mode: { enum: ["operator", "development_TEST_ONLY"] },
    reviewers: {
      ...list(
        object({
          organization: { type: "string", minLength: 1, maxLength: 96 },
          public_key: id,
        }),
        5,
      ),
      minItems: 5,
    },
  });
  d.qualification_campaign = object({
    v: constant("1"),
    network_id: id,
    source_revision: id,
    mode: { enum: ["real", "development_TEST_ONLY"] },
    installation: optionalArtifact,
    receipts: object({
      nominal: optionalArtifact,
      peak: optionalArtifact,
      hot_lane: optionalArtifact,
      raw_samples: list(artifact, 64),
    }),
    verification: object({
      envelopes: uint,
      full_checks_per_second: uint,
      backlog_recovered_ms: uint,
      certified_after_close_ms: uint,
      challenge_window_ms: uint,
      independent_implementations: list(
        { type: "string", minLength: 1, maxLength: 96 },
        8,
      ),
      evidence: optionalArtifact,
    }),
    mobile: object({
      physical: { type: "boolean" },
      generation_p95_ms: uint,
      evidence: optionalArtifact,
    }),
    scenarios: list(
      object({
        name: { type: "string", minLength: 1, maxLength: 96 },
        passed: { type: "boolean" },
        evidence: artifact,
      }),
      64,
    ),
    audits: list(
      object({
        kind: { enum: ["cryptographic", "application"] },
        organization: { type: "string", minLength: 1, maxLength: 96 },
        source_revision: id,
        report: artifact,
      }),
      16,
    ),
    findings: list(
      object({
        id: { type: "string", minLength: 1, maxLength: 96 },
        severity: { enum: ["critical", "high", "medium", "low"] },
        status: { enum: ["open", "fixed", "accepted"] },
        remediation: optionalArtifact,
      }),
      1024,
    ),
    attestations: list(
      object({
        public_key: id,
        signature: { type: "string", pattern: "^[0-9a-f]{128}$" },
      }),
      5,
    ),
  });
  d.qualification_receipt_report = object({
    format: constant("civic-receipt-measurements-1"),
    network_id: id,
    poll_id: id,
    window_start_ms: uint,
    window_end_ms: uint,
    partitions: uint,
    attempts: uint,
    verified_unique_receipts: uint,
    duplicates: uint,
    failed_attempts: uint,
    receipt_p95_ms: uint,
    receipt_p99_ms: uint,
    receipt_max_ms: uint,
    histogram_cap_ms: constant("60000"),
    minimum_receipts_per_second: uint,
    per_second: list(uint, 86400),
    per_lane: list(uint, 256),
    average_envelope_bytes: uint,
    maximum_envelope_bytes: uint,
    public_qualified: constant(false),
  });
}
