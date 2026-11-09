import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { Ajv2020 } from "ajv/dist/2020.js";
import { installValidators } from "../src/pwa/protocol.js";
import { inspectExplorerBlock, explorerHeight } from "../src/pwa/explorer.js";
const ajv = new Ajv2020({ strict: true });
ajv.addSchema(
  JSON.parse(readFileSync("spec/schemas/protocol.schema.json", "utf8")),
);
ajv.addSchema(
  JSON.parse(readFileSync("spec/schemas/archive_block.schema.json", "utf8")),
);
installValidators({
  v_archive_block: ajv.getSchema("https://civora.invalid/v0.1/archive_block")!,
});
const fixture = JSON.parse(
  readFileSync("spec/fixtures/v0.6/archive.json", "utf8"),
);
const run = fixture.runs[0];
const genesis = JSON.parse(Buffer.from(run.genesis, "base64url").toString());

test("explorer authenticates native headers and both empty and nonempty transaction roots", () => {
  for (const bundle of run.blocks) {
    const native = JSON.parse(
      Buffer.from(bundle.block, "base64url").toString(),
    );
    const inspected = inspectExplorerBlock(
      genesis,
      bundle,
      native.header.height,
    );
    assert.equal(inspected.header.height, native.header.height);
    assert.equal(inspected.transactions.length, native.data.txs?.length ?? 0);
    assert(inspected.signatures >= 5);
    assert.match(inspected.hash, /^[0-9a-f]{64}$/);
  }
});
test("explorer refuses a relabelled height, foreign genesis, altered header and altered transaction bytes", () => {
  const bundle = run.blocks[0];
  assert.throws(() => inspectExplorerBlock(genesis, bundle, "2"));
  assert.throws(() =>
    inspectExplorerBlock({ ...genesis, chain_id: "foreign" }, bundle, "1"),
  );
  for (const attack of ["header", "transaction"]) {
    const native = JSON.parse(
      Buffer.from(bundle.block, "base64url").toString(),
    );
    if (attack === "header") native.header.app_hash = "00".repeat(32);
    else
      native.data.txs[0] = Buffer.from("tampered public bytes").toString(
        "base64",
      );
    const bad = {
      ...bundle,
      block: Buffer.from(JSON.stringify(native)).toString("base64url"),
    };
    assert.throws(() => inspectExplorerBlock(genesis, bad, "1"));
  }
});
test("explorer height selection keeps uint64 precision and refuses invalid or unavailable heights", () => {
  assert.equal(
    explorerHeight("18446744073709551615", "18446744073709551615"),
    "18446744073709551615",
  );
  for (const value of [
    "0",
    "-1",
    "01",
    "1e2",
    "1/blocks",
    "18446744073709551616",
    "11",
  ])
    assert.throws(() => explorerHeight(value, "10"));
});
