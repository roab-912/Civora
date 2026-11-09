// Run against an explicitly started local HTTPS bridge. Only backup creation is
// exercised: no account is registered and no valid transaction is submitted.
import assert from "node:assert/strict";
import { Agent, request } from "node:https";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
const args = process.argv.slice(2),
  get = (key, fallback) => {
    const i = args.indexOf(key);
    return i < 0 ? fallback : args[i + 1];
  },
  origin = new URL(get("--url")),
  agent = new Agent({
    ca: readFileSync(get("--ca", ".tools/pwa/lan-tls/ca.pem")),
  });
if (origin.protocol !== "https:")
  throw Error("Pass --url https://LAN_ADDRESS:PORT");
async function call(path, body, requestOrigin = origin.origin) {
  return new Promise((yes, no) => {
    const req = request(
      new URL(path, origin),
      {
        agent,
        method: body ? "POST" : "GET",
        headers: body
          ? { "Content-Type": "application/json", Origin: requestOrigin }
          : {},
      },
      (res) => {
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () =>
          yes({
            status: res.statusCode,
            body: Buffer.concat(chunks).toString(),
          }),
        );
      },
    );
    req.on("error", no);
    req.end(body);
  });
}
try {
  assert.equal((await call("/")).status, 200);
  const configuration = await call("/pwa-config.json");
  assert.equal(configuration.status, 200);
  const apiRoot = JSON.parse(configuration.body).networks[0].api_roots[0];
  assert.equal(
    (await call(apiRoot + "/v1/transactions", "{}", "https://foreign.invalid"))
      .status,
    403,
  );
  assert.equal((await call(apiRoot + "/v1/transactions", "{}")).status, 422);
  process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
  const { chromium, expect } = await import("@playwright/test"),
    browser = await chromium.launch({ headless: true });
  try {
    // Scoped to this disposable test context. Node above validates the real CA,
    // certificate chain and IP; actual clients install that CA normally.
    const context = await browser.newContext({
        ignoreHTTPSErrors: true,
        acceptDownloads: true,
      }),
      page = await context.newPage();
    await page.goto(origin.origin);
    await expect(page.locator("#connection")).toHaveAttribute(
      "data-state",
      "verified",
      {
        timeout: 25000,
      },
    );
    assert(await page.evaluate(() => isSecureContext && !!crypto.subtle));
    await page.getByRole("link", { name: "Mon compte", exact: false }).click();
    await page
      .getByRole("button", { name: "Créer un compte", exact: true })
      .click();
    await expect(page.getByTestId("backup-code")).toHaveText(/^[0-9a-f]{64}$/);
    const event = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Télécharger ma sauvegarde chiffrée" })
      .click();
    const backup = JSON.parse(readFileSync(await (await event).path(), "utf8"));
    assert.equal(backup.header.v, "1");
    assert(
      backup.ciphertext && !JSON.stringify(backup).includes("signing_seed"),
    );
    console.log(
      JSON.stringify({
        passed: true,
        url: origin.origin,
        tls_ca_and_ip_verified: true,
        foreign_origin_refused: true,
        same_origin_post_forwarded: true,
        web_crypto_available: true,
        encrypted_backup_created_and_downloaded: true,
        account_registered: false,
      }),
    );
  } finally {
    await browser.close();
  }
} finally {
  agent.destroy();
}
