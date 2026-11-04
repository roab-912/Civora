// Civic envelopes only. External ElectionGuard schemas remain unqualified.
export function electionguardSchemas(d, { object, list, id, constant }) {
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
}
