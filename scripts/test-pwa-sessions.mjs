// Real TEST_ONLY workshops and a persistent browser origin. Never touch the
// user's workshop, browser profile, vault, or stored rollback protection.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { resolve } from "node:path";
import { readFileSync } from "node:fs";
import { domain } from "../dist/web/src/protocol.js";

process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
const { chromium, expect } = await import("@playwright/test");
const servers = [];
let browser;

async function workshop() {
  const child = spawn(
    process.execPath,
    ["scripts/serve-pwa.mjs", "--port", "0"],
    {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe", "ipc"],
    },
  );
  servers.push(child);
  return new Promise((yes, no) => {
    let log = "";
    const timer = setTimeout(
      () => no(Error("Workshop startup: " + log)),
      120000,
    );
    const append = (data) => {
      log += data;
      const match = /PUBLIC_TEST_ONLY_PWA (http:\/\/127\.0\.0\.1:\d+)/.exec(
        log,
      );
      if (match) {
        clearTimeout(timer);
        yes(match[1]);
      }
    };
    child.stdout.on("data", append);
    child.stderr.on("data", append);
    child.once("error", (error) => {
      clearTimeout(timer);
      no(error);
    });
    child.once("exit", () => {
      clearTimeout(timer);
      no(Error(log));
    });
  });
}

try {
  const first = await workshop();
  const firstConfig = await (await fetch(first + "/pwa-config.json")).json();
  const firstNetwork = firstConfig.networks[0].manifest.network_id;
  const legacy = domain("development/pwa", "PUBLIC_TEST_ONLY_V0_7");
  assert.notEqual(firstNetwork, legacy);
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ serviceWorkers: "block" });
  const page = await context.newPage();
  await page.goto(first);
  await expect(page.locator("#connection")).toHaveAttribute(
    "data-state",
    "verified",
  );
  await expect(page.locator("#release-version")).toHaveText(
    "Expérimental · v" +
      JSON.parse(readFileSync("package.json", "utf8")).version,
  );
  await expect(page.locator("#notice")).toContainText("mode hors ligne");

  // Simulate a browser that has already seen more blocks on this exact network.
  const high = "999999";
  await page.evaluate(
    async ({ firstNetwork, legacy, high }) => {
      const db = await new Promise((yes, no) => {
        const req = indexedDB.open("civora-pwa-1", 1);
        req.onsuccess = () => yes(req.result);
        req.onerror = () => no(req.error);
      });
      await new Promise((yes, no) => {
        const tx = db.transaction(
          ["settings", "vault", "public", "pending"],
          "readwrite",
        );
        tx.objectStore("settings").put(high, "height/" + firstNetwork);
        tx.objectStore("settings").put(high, "height/" + legacy);
        for (const store of ["vault", "public", "pending"])
          tx.objectStore(store).put("TEST_ONLY_PRESERVE", "session-regression");
        tx.oncomplete = yes;
        tx.onerror = () => no(tx.error);
      });
      db.close();
    },
    { firstNetwork, legacy, high },
  );
  await page.reload();
  await expect(page.locator("#notice")).toContainText("ERR_ROLLBACK");
  await expect(page.locator("#notice")).toContainText("hauteur inférieure");
  await expect(page.locator("#connection")).toHaveAttribute(
    "data-state",
    "blocked",
  );

  const second = await workshop();
  const secondConfig = await (await fetch(second + "/pwa-config.json")).json();
  const secondNetwork = secondConfig.networks[0].manifest.network_id;
  assert.notEqual(secondNetwork, firstNetwork);
  assert.notEqual(secondNetwork, legacy);
  assert.equal(firstConfig.networks.length, 1);
  assert.equal(secondConfig.networks.length, 1);
  assert.equal((await fetch(first + "/api/archive/v1/network")).status, 404);
  // Keep the browser origin and all its storage, replace only its server.
  await page.route(first + "/pwa-config.json", async (route) => {
    await route.fulfill({ json: secondConfig });
  });
  await page.route(first + "/api/atelier/**", async (route) => {
    const original = new URL(route.request().url());
    const response = await route.fetch({
      url: second + original.pathname + original.search,
    });
    await route.fulfill({ response });
  });
  await page.reload();
  await expect(page.locator("#connection")).toHaveAttribute(
    "data-state",
    "verified",
  );
  await expect(page.locator("#notice")).toContainText("mode hors ligne");
  await expect(page.locator("#notice")).not.toContainText("ERR_ROLLBACK");
  const stored = await page.evaluate(
    async ({ firstNetwork, secondNetwork, legacy }) => {
      const db = await new Promise((yes, no) => {
        const req = indexedDB.open("civora-pwa-1", 1);
        req.onsuccess = () => yes(req.result);
        req.onerror = () => no(req.error);
      });
      const get = (store, key) =>
        new Promise((yes, no) => {
          const req = db.transaction(store).objectStore(store).get(key);
          req.onsuccess = () => yes(req.result);
          req.onerror = () => no(req.error);
        });
      const values = await Promise.all([
        get("settings", "height/" + firstNetwork),
        get("settings", "height/" + legacy),
        get("settings", "height/" + secondNetwork),
        ...["vault", "public", "pending"].map((store) =>
          get(store, "session-regression"),
        ),
      ]);
      db.close();
      return values;
    },
    { firstNetwork, secondNetwork, legacy },
  );
  assert.equal(stored[0], high, "never lower the previous network's floor");
  assert.equal(stored[1], high, "never clear the historical network's floor");
  assert(BigInt(stored[2]) < BigInt(high));
  assert.deepEqual(stored.slice(3), Array(3).fill("TEST_ONLY_PRESERVE"));
  console.log(
    "PASS: distinct workshop networks, unchanged archive, same-network rollback refused, old heights/vault/cache/pending preserved, release label 1.0.0",
  );
} finally {
  await browser?.close();
  await Promise.all(
    servers.map(async (child) => {
      if (child.exitCode !== null) return;
      const done = new Promise((yes) => child.once("exit", yes));
      if (child.connected) child.send({ type: "shutdown" });
      else child.kill();
      await done;
    }),
  );
}
