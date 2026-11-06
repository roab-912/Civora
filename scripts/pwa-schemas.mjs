export function pwaSchemas(d, { object, list, id, constant }) {
  const seed = id,
    text = { type: "string", minLength: 1, maxLength: 4096 },
    blob = (size) => ({
      type: "string",
      pattern: "^[A-Za-z0-9_-]+$",
      maxLength: size,
    });
  d.pwa_local_account = object({
    network_id: id,
    account_id: id,
    signing_seed: seed,
    identity: text,
    prior_identities: list(text, 32),
  });
  d.pwa_backup_secrets = object({
    ...d.pwa_local_account.properties,
    recovery_seed: seed,
  });
  d.pwa_backup = object({
    header: object({ v: constant("1"), network_id: id, account_id: id }),
    salt: { ...blob(43), minLength: 43 },
    iv: { ...blob(16), minLength: 16 },
    ciphertext: blob(262144),
  });
}
