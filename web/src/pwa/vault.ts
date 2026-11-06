import { Identity } from "@semaphore-protocol/identity";
import { Buffer } from "./buffer.js";
import {
  canonical,
  decode,
  domain,
  blob,
  randomHex,
  publicKey,
} from "./protocol.js";
import { read, write, remove, insertIfAbsent } from "./storage.js";
export function requireWebCrypto(): SubtleCrypto {
  if (!globalThis.crypto?.subtle) throw Error("ERR_SECURE_CONTEXT");
  return globalThis.crypto.subtle;
}
export interface LocalAccount {
  network_id: string;
  account_id: string;
  signing_seed: string;
  identity: string;
  prior_identities: string[];
}
export interface BackupSecrets extends LocalAccount {
  recovery_seed: string;
}
export interface Backup {
  header: { v: string; network_id: string; account_id: string };
  salt: string;
  iv: string;
  ciphertext: string;
}
export interface PublicAccount {
  signing_pk: string;
  recovery_pk: string;
  commitment: string;
  status: string;
  next_nonce: string;
}
export function credentials(s: LocalAccount, recovery?: string) {
  return {
    signing_pk: publicKey(s.signing_seed),
    recovery_pk: recovery ? publicKey(recovery) : "",
    commitment: Identity.import(s.identity).commitment.toString(),
  };
}
export function newAccount(network: string, accountID?: string): BackupSecrets {
  const signing = randomHex(32),
    id = new Identity();
  return {
    network_id: network,
    account_id: accountID ?? domain("account", [network, publicKey(signing)]),
    signing_seed: signing,
    recovery_seed: randomHex(32),
    identity: id.export(),
    prior_identities: [],
  };
}
const bytes = (v: Uint8Array) => new Uint8Array(v).buffer;
async function backupKey(
  secret: string,
  salt: Uint8Array,
  uses: KeyUsage[],
): Promise<CryptoKey> {
  if (!/^[0-9a-f]{64}$/.test(secret)) throw Error("ERR_BACKUP_CODE");
  const material = await requireWebCrypto().importKey(
    "raw",
    bytes(Buffer.from(secret, "hex")),
    "HKDF",
    false,
    ["deriveKey"],
  );
  return requireWebCrypto().deriveKey(
    {
      name: "HKDF",
      hash: "SHA-256",
      salt: bytes(salt),
      info: bytes(new TextEncoder().encode("CIVIC/1/backup")),
    },
    material,
    { name: "AES-GCM", length: 256 },
    false,
    uses,
  );
}
export async function exportBackup(
  secrets: BackupSecrets,
): Promise<{ backup: Backup; code: string }> {
  const code = randomHex(32),
    salt = crypto.getRandomValues(new Uint8Array(32)),
    iv = crypto.getRandomValues(new Uint8Array(12)),
    header = {
      v: "1",
      network_id: secrets.network_id,
      account_id: secrets.account_id,
    },
    key = await backupKey(code, salt, ["encrypt"]),
    cipher = await requireWebCrypto().encrypt(
      {
        name: "AES-GCM",
        iv: bytes(iv),
        additionalData: bytes(new TextEncoder().encode(canonical(header))),
      },
      key,
      bytes(new TextEncoder().encode(canonical(secrets))),
    );
  return {
    code,
    backup: {
      header,
      salt: Buffer.from(salt).toString("base64url"),
      iv: Buffer.from(iv).toString("base64url"),
      ciphertext: Buffer.from(cipher).toString("base64url"),
    },
  };
}
export async function importBackup(
  raw: string,
  code: string,
  network: string,
  actual: PublicAccount | null,
): Promise<BackupSecrets> {
  const b = decode<Backup>("pwa_backup", raw);
  if (b.header.network_id !== network) throw Error("ERR_NETWORK");
  try {
    const key = await backupKey(code, blob(b.salt), ["decrypt"]),
      clear = await requireWebCrypto().decrypt(
        {
          name: "AES-GCM",
          iv: bytes(blob(b.iv)),
          additionalData: bytes(new TextEncoder().encode(canonical(b.header))),
        },
        key,
        bytes(blob(b.ciphertext)),
      ),
      s = decode<BackupSecrets>("pwa_backup_secrets", new Uint8Array(clear));
    if (s.network_id !== network || s.account_id !== b.header.account_id)
      throw Error("ERR_BACKUP_IDENTITY");
    const c = credentials(s, s.recovery_seed);
    if (
      actual &&
      (actual.signing_pk !== c.signing_pk ||
        actual.recovery_pk !== c.recovery_pk ||
        actual.commitment !== c.commitment)
    )
      throw Error("ERR_BACKUP_IDENTITY");
    if (!actual && domain("account", [network, c.signing_pk]) !== s.account_id)
      throw Error("ERR_BACKUP_IDENTITY");
    return s;
  } catch (e) {
    if (e instanceof Error && e.message.startsWith("ERR_")) throw e;
    throw Error("ERR_BACKUP_DECRYPT");
  }
}
async function deviceKey(): Promise<CryptoKey> {
  let key = await read<CryptoKey>("vault", "device-key");
  if (!key) {
    key = await requireWebCrypto().generateKey(
      { name: "AES-GCM", length: 256 },
      false,
      ["encrypt", "decrypt"],
    );
    key = await insertIfAbsent("vault", "device-key", key);
  }
  return key;
}
export async function saveAccount(
  secrets: LocalAccount,
  backup: Backup,
): Promise<void> {
  const value: LocalAccount = {
      network_id: secrets.network_id,
      account_id: secrets.account_id,
      signing_seed: secrets.signing_seed,
      identity: secrets.identity,
      prior_identities: [...secrets.prior_identities],
    },
    iv = crypto.getRandomValues(new Uint8Array(12)),
    aad = new TextEncoder().encode(
      canonical([value.network_id, value.account_id]),
    ),
    cipher = await requireWebCrypto().encrypt(
      { name: "AES-GCM", iv: bytes(iv), additionalData: bytes(aad) },
      await deviceKey(),
      bytes(new TextEncoder().encode(canonical(value))),
    );
  await write("vault", "account/" + secrets.network_id, {
    account_id: value.account_id,
    iv,
    cipher,
    backup,
  });
}
export async function loadAccount(
  network: string,
): Promise<LocalAccount | null> {
  const value = await read<{
    account_id: string;
    iv: Uint8Array;
    cipher: ArrayBuffer;
  }>("vault", "account/" + network);
  if (!value) return null;
  const plain = await requireWebCrypto().decrypt(
    {
      name: "AES-GCM",
      iv: bytes(value.iv),
      additionalData: bytes(
        new TextEncoder().encode(canonical([network, value.account_id])),
      ),
    },
    await deviceKey(),
    value.cipher,
  );
  return decode<LocalAccount>("pwa_local_account", new Uint8Array(plain));
}
export async function storedBackup(
  network: string,
): Promise<Backup | undefined> {
  return (await read<{ backup: Backup }>("vault", "account/" + network))
    ?.backup;
}
export async function forgetAccount(network: string) {
  await remove("vault", "account/" + network);
}
export async function encryptPending(
  network: string,
  poll: string,
  value: unknown,
) {
  const iv = crypto.getRandomValues(new Uint8Array(12)),
    aad = new TextEncoder().encode(canonical([network, poll])),
    cipher = await requireWebCrypto().encrypt(
      { name: "AES-GCM", iv: bytes(iv), additionalData: bytes(aad) },
      await deviceKey(),
      bytes(new TextEncoder().encode(canonical(value))),
    );
  await write("pending", network + "/" + poll, { iv, cipher });
}
export async function pending<T>(
  network: string,
  poll: string,
): Promise<T | undefined> {
  const value = await read<{ iv: Uint8Array; cipher: ArrayBuffer }>(
    "pending",
    network + "/" + poll,
  );
  if (!value) return;
  const raw = await requireWebCrypto().decrypt(
    {
      name: "AES-GCM",
      iv: bytes(value.iv),
      additionalData: bytes(
        new TextEncoder().encode(canonical([network, poll])),
      ),
    },
    await deviceKey(),
    value.cipher,
  );
  return JSON.parse(new TextDecoder().decode(raw)) as T;
}
