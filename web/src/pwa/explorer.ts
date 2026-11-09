import { Buffer } from "./buffer.js";
import { blob, parse, sha, uint, validate } from "./protocol.js";
import { logRoot, verifyHeader, type Header } from "../consensus.js";
import { trustAt, type Row } from "./network.js";

export interface ExplorerBlock {
  header: Header;
  hash: string;
  signatures: number;
  transactions: { sha256: string; type: string; value: unknown }[];
}

// This authenticates the header and transaction bytes, not execution results
// or a full replay of the chain. The observer performs that separate operation.
export function inspectExplorerBlock(
  genesis: Row,
  value: unknown,
  height: string,
): ExplorerBlock {
  if (uint(height) === 0n) throw Error("ERR_EXPLORER_HEIGHT");
  const bundle = validate<{ block: string; signed_header: string }>(
    "archive_block",
    value,
  );
  const trust = trustAt(genesis, height);
  const header = verifyHeader(trust, bundle.signed_header);
  if (header.height !== height) throw Error("ERR_PROOF_HEIGHT");
  const raw = blob(bundle.block);
  if (raw.length > 4 * 1024 * 1024) throw Error("ERR_CAPACITY");
  const block = JSON.parse(raw.toString()) as Row;
  const signed = JSON.parse(blob(bundle.signed_header).toString()) as Row;
  // Bind the native block's header to the same signed commit, without inventing
  // a second JSON canonicalization or header hashing implementation.
  verifyHeader(
    trust,
    Buffer.from(JSON.stringify({ ...signed, header: block.header })).toString(
      "base64url",
    ),
  );
  const encoded = block.data?.txs ?? [];
  if (!Array.isArray(encoded) || encoded.length > 10000)
    throw Error("ERR_CAPACITY");
  const hashes: Uint8Array[] = [];
  const transactions = encoded.map((text: unknown) => {
    if (typeof text !== "string") throw Error("ERR_ENCODING");
    const bytes = Buffer.from(text, "base64");
    if (!bytes.length || bytes.toString("base64") !== text)
      throw Error("ERR_ENCODING");
    const hash = sha(bytes);
    hashes.push(hash);
    let value: unknown = { bytes_base64: text },
      type = "Format non interprété";
    try {
      const parsed = parse(bytes) as Row;
      value = parsed;
      if (typeof parsed?.body?.type === "string") type = parsed.body.type;
    } catch {
      /* Committed bytes may include a transaction rejected by execution. */
    }
    return { sha256: hash.toString("hex"), type, value };
  });
  if (logRoot(hashes) !== header.data_hash.toLowerCase())
    throw Error("ERR_PROOF_DATA_HASH");
  return {
    header,
    hash: signed.commit.block_id.hash.toLowerCase(),
    signatures: signed.commit.signatures.filter(
      (s: Row) => s.block_id_flag === 2,
    ).length,
    transactions,
  };
}

export function explorerHeight(value: string, maximum: string): string {
  try {
    const height = uint(value);
    if (height < 1n || height > uint(maximum))
      throw Error("ERR_EXPLORER_HEIGHT");
    return value;
  } catch {
    throw Error("ERR_EXPLORER_HEIGHT");
  }
}
