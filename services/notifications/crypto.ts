// RFC 8291 single-record aes128gcm and RFC 8292 ES256. The primitives are
// provided by Node/OpenSSL; voting keys and voting cryptography are unrelated.
import {
  createECDH,
  createCipheriv,
  createHmac,
  randomBytes,
  createPrivateKey,
  sign,
} from "node:crypto";
export const b64 = (bytes: Uint8Array) =>
  Buffer.from(bytes).toString("base64url");
export function unb64(s: string, length: number): Buffer {
  const b = Buffer.from(s, "base64url");
  if (b.length !== length || b64(b) !== s) throw Error("ERR_PUSH_KEY");
  return b;
}
const hmac = (key: Uint8Array, data: Uint8Array) =>
  createHmac("sha256", key).update(data).digest();
const expand = (key: Uint8Array, info: Uint8Array, n: number) =>
  hmac(key, Buffer.concat([info, Buffer.from([1])])).subarray(0, n);
export function encryptionKeys(
  ua: Uint8Array,
  auth: Uint8Array,
  privateKey: Uint8Array,
  salt: Uint8Array,
) {
  if (
    ua.length !== 65 ||
    ua[0] !== 4 ||
    auth.length !== 16 ||
    salt.length !== 16
  )
    throw Error("ERR_PUSH_KEY");
  const e = createECDH("prime256v1");
  e.setPrivateKey(privateKey);
  const pub = e.getPublicKey(),
    shared = e.computeSecret(ua),
    ikm = expand(
      hmac(auth, shared),
      Buffer.concat([Buffer.from("WebPush: info\0"), ua, pub]),
      32,
    ),
    prk = hmac(salt, ikm);
  shared.fill(0);
  ikm.fill(0);
  return {
    publicKey: pub,
    key: expand(prk, Buffer.from("Content-Encoding: aes128gcm\0"), 16),
    nonce: expand(prk, Buffer.from("Content-Encoding: nonce\0"), 12),
  };
}
export function encryptPush(
  payload: Uint8Array,
  keys: { p256dh: string; auth: string },
): Buffer {
  if (payload.length > 3993) throw Error("ERR_PUSH_CAPACITY");
  const e = createECDH("prime256v1");
  e.generateKeys();
  const salt = randomBytes(16),
    privateKey = e.getPrivateKey(),
    derived = encryptionKeys(
      unb64(keys.p256dh, 65),
      unb64(keys.auth, 16),
      privateKey,
      salt,
    ),
    cipher = createCipheriv("aes-128-gcm", derived.key, derived.nonce),
    encrypted = Buffer.concat([
      cipher.update(Buffer.concat([payload, Buffer.from([2])])),
      cipher.final(),
      cipher.getAuthTag(),
    ]),
    header = Buffer.alloc(86);
  salt.copy(header);
  header.writeUInt32BE(4096, 16);
  header[20] = 65;
  derived.publicKey.copy(header, 21);
  privateKey.fill(0);
  derived.key.fill(0);
  derived.nonce.fill(0);
  return Buffer.concat([header, encrypted]);
}
export function vapidPublic(secret: string): string {
  const e = createECDH("prime256v1");
  e.setPrivateKey(unb64(secret, 32));
  return b64(e.getPublicKey());
}
export function newVapid(): string {
  const e = createECDH("prime256v1");
  e.generateKeys();
  return encodeP256Private(e.getPrivateKey());
}
export function encodeP256Private(raw: Uint8Array): string {
  if (!raw.length || raw.length > 32) throw Error("ERR_VAPID_KEY");
  return b64(Buffer.concat([Buffer.alloc(32 - raw.length), raw]));
}
export function vapidAuthorization(
  secret: string,
  endpoint: string,
  contact: string,
  now: number,
): string {
  const pub = unb64(vapidPublic(secret), 65),
    url = new URL(contact);
  if (!["mailto:", "https:"].includes(url.protocol))
    throw Error("ERR_VAPID_CONTACT");
  const key = createPrivateKey({
      key: {
        kty: "EC",
        crv: "P-256",
        x: b64(pub.subarray(1, 33)),
        y: b64(pub.subarray(33)),
        d: secret,
      },
      format: "jwk",
    }),
    jwt =
      b64(Buffer.from(JSON.stringify({ typ: "JWT", alg: "ES256" }))) +
      "." +
      b64(
        Buffer.from(
          JSON.stringify({
            aud: new URL(endpoint).origin,
            exp: Math.floor(now / 1000) + 12 * 3600,
            sub: contact,
          }),
        ),
      ),
    signature = sign("sha256", Buffer.from(jwt), {
      key,
      dsaEncoding: "ieee-p1363",
    });
  return `vapid t=${jwt}.${b64(signature)}, k=${b64(pub)}`;
}
