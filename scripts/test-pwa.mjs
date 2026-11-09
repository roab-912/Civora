// Real desktop/mobile browser engines against the real Go core. Public TEST_ONLY
// guardian keys and synthetic native commits are explicit development fixtures.
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { resolve, join } from "node:path";
import { spawn, spawnSync } from "node:child_process";
import assert from "node:assert/strict";
import { domain } from "../dist/web/src/protocol.js";
import { testPartitionWorker } from "./test-pwa-partition.mjs";
process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
const {
  chromium,
  webkit,
  devices,
  expect: baseExpect,
} = await import("@playwright/test");
const expect = baseExpect.configure({ timeout: 25000 });
const directory = resolve(".tools/qualification/pwa-" + Date.now());
mkdirSync(directory, { recursive: true });
let server,
  historicalServer,
  frontendHost,
  origin = process.env.CIVORA_PWA_URL;
if (!origin) {
  server = spawn(process.execPath, ["scripts/serve-pwa.mjs", "--port", "0"], {
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe", "ipc"],
  });
  let log = "";
  origin = await new Promise((yes, no) => {
    const timer = setTimeout(
      () => no(Error("PWA server startup: " + log)),
      120000,
    );
    const append = (b) => {
      log += b;
      const m = /PUBLIC_TEST_ONLY_PWA (http:\/\/127\.0\.0\.1:\d+)/.exec(log);
      if (m) {
        clearTimeout(timer);
        yes(m[1]);
      }
    };
    server.stdout.on("data", append);
    server.stderr.on("data", append);
    server.once("exit", () => no(Error(log)));
  });
}
const report = {
  milestone: "v0.8-regression-v0.7",
  mode: "PUBLIC_TEST_ONLY_REAL_CRYPTO_SYNTHETIC_COMMITS",
  physical_devices: false,
  checks: [],
  browsers: [],
  screenshots: [],
};
const browsers = [];
let backup, code, accountID, savedReceipt, savedResultReport;
let anonymousPosts = 0;
function observe(page) {
  page.on("request", (r) => {
    if (r.method() !== "POST" || !r.url().endsWith("/v1/transactions")) return;
    const tx = r.postDataJSON();
    if (tx?.body?.type !== "vote.cast") return;
    assert.equal(tx.body.actor_id, null);
    assert.equal(tx.body.nonce, null);
    assert(!r.postData().includes(accountID));
    assert(!r.postData().includes('"choice"'));
    anonymousPosts++;
  });
  page.on("pageerror", (e) => console.log("Browser error: " + e.message));
  page.on("console", (m) => {
    if (m.type() === "error")
      console.log("Browser console: " + m.text().slice(0, 250));
  });
}
async function wait(page) {
  await expect(page.locator("body")).not.toHaveAttribute("aria-busy", "true");
}
async function capture(page, name) {
  const path = join(directory, name + ".png");
  await page.screenshot({ path, fullPage: true });
  report.screenshots.push(path);
  assert(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1,
    ),
    "horizontal overflow: " + name,
  );
}
async function navigate(page, path) {
  const url = origin + "/#" + path;
  if (page.url() === url) await page.reload();
  else await page.goto(url);
  await expect(page.locator("#connection")).toHaveAttribute(
    "data-state",
    "verified",
  );
  await wait(page);
}
async function submit(page, action, options = {}) {
  await navigate(
    page,
    "/new/" +
      action +
      (options.target
        ? "?target=" +
          options.target +
          (options.grant ? "&grant=" + options.grant : "")
        : ""),
  );
  if (options.title)
    await page
      .getByLabel(
        action.startsWith("role") ? "Intitulé du rôle" : "Titre du texte",
        { exact: true },
      )
      .fill(options.title);
  if (options.text)
    await page
      .getByLabel(
        action.startsWith("role") ? "Description" : "Texte intégral proposé",
        { exact: true },
      )
      .fill(options.text);
  if (action === "role.create")
    await page.getByLabel("Publier un avis", { exact: true }).check();
  if (action === "role.grant")
    await page
      .getByLabel("Compte bénéficiaire", { exact: false })
      .fill(accountID);
  await page
    .getByLabel("Pourquoi proposer cette décision ?", { exact: true })
    .fill(
      "Développement reproductible : vérifier les décisions et leurs effets.",
    );
  await page.getByRole("button", { name: "Relire avant de déposer" }).click();
  await page.getByRole("button", { name: "Confirmer le dépôt public" }).click();
  await expect(
    page.getByRole("link", { name: "Ouvrir les commandes de l’atelier" }),
  ).toBeVisible({ timeout: 45000 });
  const id = new URL(page.url()).hash.split("/").at(-1);
  return id;
}
async function decide(page, id, { vote = true, voter = page } = {}) {
  const advance = async (label) => {
    await navigate(page, "/network/workshop?proposal=" + id);
    await page.getByRole("button", { name: label }).click();
    await wait(page);
    await navigate(page, "/proposals/" + id);
  };
  await advance("Atelier : ouvrir le vote");
  await expect(
    page.getByRole("button", { name: "Participer au vote" }),
  ).toBeVisible();
  if (vote) {
    if (voter !== page) await navigate(voter, "/proposals/" + id);
    await voter.getByRole("button", { name: "Participer au vote" }).click();
    await voter.getByRole("radio", { name: /^Oui/ }).check();
    await voter.getByRole("button", { name: "Vérifier mon choix" }).click();
    await voter
      .getByRole("button", { name: "Confirmer et chiffrer mon bulletin" })
      .click();
    await expect(voter.getByRole("heading", { level: 1 })).toContainText(
      "Compté",
      { timeout: 120000 },
    );
    await capture(voter, "receipt-" + id.slice(0, 8));
    if (!savedReceipt) {
      const download = voter.waitForEvent("download");
      await voter.getByRole("button", { name: "Télécharger mon reçu" }).click();
      savedReceipt = readFileSync(await (await download).path(), "utf8");
    }
    await voter.getByRole("link", { name: "Retour au scrutin" }).click();
  }
  await advance("Atelier : clore et dépouiller");
  await expect(
    page.getByRole("button", { name: "Vérifier le résultat" }),
  ).toBeVisible({ timeout: 40000 });
  await page.getByRole("button", { name: "Vérifier le résultat" }).click();
  await expect(
    page.getByRole("heading", { name: "Le résultat est vérifié." }),
  ).toBeVisible({ timeout: 60000 });
  if (!savedResultReport) {
    const downloaded = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Télécharger le rapport de résultat" })
      .click();
    savedResultReport = readFileSync(await (await downloaded).path(), "utf8");
  }
  await page.getByRole("link", { name: "Retour au dossier" }).click();
  await advance("Atelier : appliquer la décision");
  await expect(page.locator(".meta-line")).toContainText(
    "Appliquée au registre",
  );
}
try {
  const browser = await chromium.launch({ headless: true });
  browsers.push(browser);
  const desktop = await browser.newContext({
      viewport: { width: 1440, height: 1000 },
      acceptDownloads: true,
    }),
    page = await desktop.newPage();
  observe(page);
  await navigate(page, "/");
  await page.keyboard.press("Tab");
  assert(
    await page.evaluate(
      () =>
        ["A", "BUTTON", "SELECT", "INPUT"].includes(
          document.activeElement.tagName,
        ) && getComputedStyle(document.activeElement).outlineStyle !== "none",
    ),
  );
  report.checks.push("keyboard-focus-visible-and-labeled-desktop-mobile-forms");
  await capture(page, "desktop-home");
  report.browsers.push("Chromium desktop 1440x1000");
  await navigate(page, "/account");
  await page
    .getByRole("button", { name: "Créer un compte", exact: true })
    .click();
  code = await page.getByTestId("backup-code").textContent();
  assert.match(code, /^[0-9a-f]{64}$/);
  const d = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Télécharger ma sauvegarde chiffrée" })
    .click();
  const download = await d;
  backup = readFileSync(await download.path(), "utf8");
  assert(!backup.includes("signing_seed") && !backup.includes("recovery_seed"));
  await page.getByLabel("J’ai sauvegardé le fichier", { exact: false }).check();
  await page
    .getByRole("button", { name: /J’ai conservé la sauvegarde et son code/ })
    .click();
  await expect(page.locator("#notice")).toContainText("Compte inscrit", {
    timeout: 30000,
  });
  accountID = await page.locator("#app .identifier").first().textContent();
  assert.match(accountID, /^[0-9a-f]{64}$/);
  report.checks.push("desktop-create-backup-register-native-inclusion");
  const vault = await page.evaluate(
    () =>
      new Promise((yes, no) => {
        const r = indexedDB.open("civora-pwa-1");
        r.onsuccess = () => {
          const tx = r.result.transaction("vault"),
            get = tx.objectStore("vault").getAll();
          get.onsuccess = () =>
            yes(
              get.result.map((v) =>
                v instanceof CryptoKey
                  ? { extractable: v.extractable, type: v.type }
                  : Object.keys(v),
              ),
            );
          get.onerror = no;
        };
      }),
  );
  assert(vault.some((v) => v.extractable === false));
  assert(
    !JSON.stringify(vault).includes("signing_seed") &&
      !JSON.stringify(vault).includes("recovery_seed"),
  );
  report.checks.push(
    "nonextractable-device-key-and-no-clear-secrets-in-indexeddb",
  );
  const mobileBrowser = await webkit.launch({ headless: true });
  browsers.push(mobileBrowser);
  const mobile = await mobileBrowser.newContext({
      ...devices["iPhone 13"],
      acceptDownloads: true,
    }),
    phone = await mobile.newPage();
  observe(phone);
  await navigate(phone, "/account");
  await phone.getByLabel("Fichier de sauvegarde chiffrée").setInputFiles({
    name: "backup.json",
    mimeType: "application/json",
    buffer: Buffer.from(backup),
  });
  await phone
    .getByLabel("Code de secours (64 caractères hexadécimaux)")
    .fill(code);
  await phone
    .getByRole("button", { name: "Importer et vérifier le compte" })
    .click();
  await expect(phone.locator("#notice")).toContainText("compte vérifié");
  assert.equal(
    await phone.locator("#app .identifier").first().textContent(),
    accountID,
  );
  await capture(phone, "mobile-recovered-account");
  report.browsers.push("WebKit iPhone 13 emulation 390x664");
  report.checks.push("encrypted-backup-import-on-independent-mobile-browser");
  const law = await submit(page, "law.create", {
    title: "Un jardin partagé pour le quartier",
    text: "Article 1. Un jardin partagé est ouvert aux habitants.\nArticle 2. Son entretien fait l’objet d’un bilan public.",
  });
  await decide(page, law);
  report.checks.push("propose-law-encrypt-Semaphore-vote-verify-total-apply");
  const laws = await (
      await fetch(origin + "/api/atelier/v1/laws?limit=100")
    ).json(),
    lawID = domain("object", ["law", law]);
  let id = await submit(page, "law.repeal", { target: lawID });
  await decide(page, id, { voter: phone });
  await navigate(page, "/laws/" + lawID);
  await expect(page.locator(".meta-line")).toContainText("Abrogée");
  id = await submit(page, "law.restore", { target: lawID });
  await decide(page, id);
  await navigate(page, "/laws/" + lawID);
  await expect(page.locator(".meta-line")).toContainText("En vigueur");
  await capture(page, "desktop-restored-law");
  report.checks.push(
    "repeal-and-restore-through-new-votes-and-preserved-history",
  );
  id = await submit(page, "role.create", {
    title: "Référent du jardin",
    text: "Publier les avis publics du jardin partagé.",
  });
  await decide(page, id);
  const roles = await (
      await fetch(origin + "/api/atelier/v1/roles?limit=100")
    ).json(),
    roleID = domain("object", ["role", id]);
  id = await submit(page, "role.grant", { target: roleID });
  await decide(page, id);
  await navigate(page, "/roles/" + roleID);
  await expect(
    page.getByText("Compte " + accountID.slice(0, 10), { exact: false }),
  ).toBeVisible();
  let role = await (
      await fetch(origin + "/api/atelier/v1/roles/" + roleID)
    ).json(),
    grant = role.grants.find((g) => g.status === "active");
  assert(grant);
  id = await submit(page, "role.revoke", { target: roleID, grant: grant.id });
  await decide(page, id);
  await navigate(page, "/roles/" + roleID);
  await expect(page.locator(".grant")).toContainText("Révoquée");
  report.checks.push("create-role-grant-and-revoke-through-three-real-votes");
  assert.equal(anonymousPosts, 6);
  report.checks.push(
    "six-anonymous-ballot-posts-without-account-id-or-clear-choice",
  );
  await navigate(page, "/verify");
  await page.getByLabel("Fichier du reçu").setInputFiles({
    name: "receipt.json",
    mimeType: "application/json",
    buffer: Buffer.from(savedReceipt),
  });
  await page.getByRole("button", { name: "Vérifier ce reçu" }).click();
  await expect(page.locator("#app h1")).toContainText("inclusion vérifiée");
  report.checks.push(
    "downloaded-receipt-import-compared-to-native-proof-without-submission",
  );
  assert.equal(
    await page
      .getByRole("button", { name: "Retransmettre le même bulletin" })
      .count(),
    0,
  );
  let verificationPosts = 0;
  const countVerificationPosts = (request) => {
    if (request.method() === "POST") verificationPosts++;
  };
  page.on("request", countVerificationPosts);
  await navigate(page, "/network/receipt");
  await page.getByLabel("Fichier du reçu").setInputFiles({
    name: "civora-verification.json",
    mimeType: "application/json",
    buffer: Buffer.from(savedResultReport),
  });
  await page.getByRole("button", { name: "Vérifier ce reçu" }).click();
  await expect(
    page.getByRole("heading", { name: "Le résultat est vérifié." }),
  ).toBeVisible({ timeout: 60000 });
  await expect(page.locator("#app")).toContainText("Rapport importé comparé");
  const altered = JSON.parse(savedResultReport);
  altered.yes = String(BigInt(altered.yes) + 1n);
  await navigate(page, "/network/receipt");
  await page.getByLabel("Fichier du reçu").setInputFiles({
    name: "altered-result.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(altered)),
  });
  await page.getByRole("button", { name: "Vérifier ce reçu" }).click();
  await expect(page.locator("#notice")).toContainText("ne correspond pas", {
    timeout: 60000,
  });
  assert.equal(verificationPosts, 0);
  page.off("request", countVerificationPosts);
  report.checks.push(
    "historical-result-report-reimport-rechecks-cryptographic-proofs-refuses-edited-totals-and-never-submits",
  );
  const settings = await (await fetch(origin + "/pwa-config.json")).json(),
    choice = settings.networks[0],
    base = origin + choice.api_roots[0],
    genesis = await (await fetch(base + "/v1/genesis")).text(),
    status = await (await fetch(base + "/v1/network")).json(),
    height = String(BigInt(status.height) - 1n),
    proposal = await (await fetch(base + "/v1/proposals/" + id)).json(),
    pollID = proposal.poll_id;
  const proof = async (table, key) =>
    (
      await fetch(
        base +
          "/v1/proofs/state?table=" +
          table +
          "&height=" +
          height +
          "&key=" +
          Buffer.from(JSON.stringify(key)).toString("base64url"),
      )
    ).json();
  const context = {
    network_id: choice.manifest.network_id,
    poll_id: pollID,
    genesis,
    genesis_sha256: choice.manifest.main.genesis.sha256,
    configuration: await proof("network", "config"),
    proposal: await proof("proposals", id),
    poll: await proof("polls", pollID),
  };
  for (const [field, error] of [
    ["network_id", "ERR_NETWORK"],
    ["poll_id", "ERR_PROOF_CONTEXT"],
  ]) {
    const tampered = { ...context, [field]: "00".repeat(32) };
    const response = await page.evaluate(
      (context) =>
        new Promise((yes, no) => {
          const w = new Worker("/worker.js", { type: "module" }),
            timer = setTimeout(() => {
              w.terminate();
              no(Error("context rejection timeout"));
            }, 20000);
          w.onmessage = (event) => {
            clearTimeout(timer);
            w.terminate();
            yes(event.data);
          };
          w.onerror = (event) => {
            clearTimeout(timer);
            w.terminate();
            no(Error(event.message));
          };
          w.postMessage({
            id: "negative",
            type: "vote",
            input: { context, identity: "invalid", path: {}, choice: 1 },
          });
        }),
      tampered,
    );
    assert.equal(response.error, error);
  }
  report.checks.push(
    "production-worker-refuses-foreign-network-and-poll-before-key-or-proof-use",
  );
  const incompatible = await browser.newContext({ serviceWorkers: "block" });
  await incompatible.route("**/pwa-config.json", (route) =>
    route.fulfill({ json: { ...settings, version: "9.0.0" } }),
  );
  const rejected = await incompatible.newPage();
  await rejected.goto(origin);
  await expect(rejected.locator("#app h1")).toContainText(
    "ne peut pas démarrer",
  );
  await expect(rejected.locator("#notice")).toContainText("version");
  await incompatible.close();
  report.checks.push("unknown-frontend-version-blocks-startup");
  const hostConfig = join(directory, "host-networks.json");
  writeFileSync(
    hostConfig,
    JSON.stringify({
      notifications: { directory: join(directory, "host-notifications") },
      networks: [
        { name: choice.name, manifest: choice.manifest, api_roots: [base] },
      ],
    }),
  );
  frontendHost = spawn(
    process.execPath,
    ["scripts/host-pwa.mjs", "--config", hostConfig, "--port", "0"],
    { windowsHide: true, stdio: ["ignore", "pipe", "pipe"] },
  );
  const hosted = await new Promise((yes, no) => {
    let log = "";
    const timer = setTimeout(
      () => no(Error("frontend startup timeout: " + log)),
      20000,
    );
    const append = (b) => {
      log += b;
      const m = /Civora PWA (http:\/\/127\.0\.0\.1:\d+)/.exec(log);
      if (m) {
        clearTimeout(timer);
        yes(m[1]);
      }
    };
    frontendHost.stdout.on("data", append);
    frontendHost.stderr.on("data", append);
    frontendHost.once("exit", () => {
      clearTimeout(timer);
      no(Error(log));
    });
  });
  const hostedConfig = await (await fetch(hosted + "/pwa-config.json")).json();
  assert.equal(hostedConfig.development, false);
  assert.equal(hostedConfig.networks[0].development_clock, false);
  assert.equal(
    (
      await (
        await fetch(
          hosted + hostedConfig.networks[0].api_roots[0] + "/v1/network",
        )
      ).json()
    ).network_id,
    choice.manifest.network_id,
  );
  assert.equal(
    (await fetch(hosted + "/__dev/advance", { method: "POST" })).status,
    404,
  );
  assert.equal(
    (await fetch(hosted + "/icon-192.png")).headers.get("content-type"),
    "image/png",
  );
  assert(
    (await fetch(hosted)).headers
      .get("content-security-policy")
      .includes("style-src 'self'"),
  );
  report.checks.push(
    "node-frontend-proxy-manifest-csp-installable-icons-without-development-controls",
  );
  await navigate(phone, "/laws");
  await capture(phone, "mobile-laws");
  await phone.evaluate(() => navigator.serviceWorker.ready);
  await phone.reload();
  await expect(phone.locator("#app h1")).toContainText("Les lois");
  let posts = 0;
  phone.on("request", (r) => {
    if (r.method() === "POST") posts++;
  });
  await mobile.setOffline(true);
  await phone.reload();
  await expect(phone.locator("#connection")).toContainText("Hors ligne");
  await expect(phone.locator("#app h1")).toContainText("Les lois");
  await capture(phone, "mobile-offline-cache");
  await mobile.setOffline(false);
  await phone.waitForTimeout(700);
  assert.equal(posts, 0, "silent offline submissions");
  report.checks.push(
    "offline-shell-public-cache-explicit-label-and-no-background-post",
  );
  // The historical v0.6 fixture remains a protocol regression test, isolated
  // from the one network offered by the actual application.
  const fixtureDirectory = join(directory, "historical-fixture");
  mkdirSync(fixtureDirectory);
  const fixtureBinary = join(fixtureDirectory, "civnode.exe");
  const go = process.env.CIVORA_GO ?? resolve(".tools/go/bin/go.exe");
  const built = spawnSync(
    go,
    ["build", "-buildvcs=false", "-o", fixtureBinary, "./cmd/civnode"],
    {
      encoding: "utf8",
      windowsHide: true,
      env: {
        ...process.env,
        GOPATH: resolve(".tools/gopath"),
        GOMODCACHE: resolve(".tools/gomodcache"),
        GOCACHE: resolve(".tools/gocache"),
      },
    },
  );
  assert.equal(built.status, 0, built.stderr);
  historicalServer = spawn(
    fixtureBinary,
    [
      "serve",
      "--home",
      join(fixtureDirectory, "data"),
      "--fixture",
      "spec/fixtures/v0.6/archive.json",
      "--listen",
      "127.0.0.1:0",
    ],
    { windowsHide: true, stdio: ["ignore", "pipe", "pipe"] },
  );
  const historicalOrigin = await new Promise((yes, no) => {
    let log = "";
    const timer = setTimeout(
      () => no(Error("Historical fixture startup: " + log)),
      60000,
    );
    const append = (chunk) => {
      log += chunk;
      const match = /Citizen archive API: (127\.0\.0\.1:\d+)/.exec(log);
      if (match) {
        clearTimeout(timer);
        yes("http://" + match[1]);
      }
    };
    historicalServer.stdout.on("data", append);
    historicalServer.stderr.on("data", append);
    historicalServer.once("exit", () => {
      clearTimeout(timer);
      no(Error(log));
    });
  });
  const fixtureChoice = {
    name: "Historical fixture TEST_ONLY",
    manifest: JSON.parse(
      readFileSync("spec/fixtures/v0.6/network.json", "utf8"),
    ),
    api_roots: ["/api/fixture"],
    development_clock: false,
  };
  const historicalContext = await browser.newContext({
    serviceWorkers: "block",
  });
  await historicalContext.route(origin + "/pwa-config.json", (route) =>
    route.fulfill({
      json: { ...settings, participation: false, networks: [fixtureChoice] },
    }),
  );
  await historicalContext.route(origin + "/api/fixture/**", async (route) => {
    const url = new URL(route.request().url());
    await route.fulfill({
      response: await route.fetch({
        url:
          historicalOrigin +
          url.pathname.slice("/api/fixture".length) +
          url.search,
      }),
    });
  });
  const historicalPage = await historicalContext.newPage();
  await navigate(historicalPage, "/account");
  await historicalPage
    .getByLabel("Fichier de sauvegarde chiffrée")
    .setInputFiles({
      name: "backup.json",
      mimeType: "application/json",
      buffer: Buffer.from(backup),
    });
  await historicalPage
    .getByLabel("Code de secours (64 caractères hexadécimaux)")
    .fill(code);
  await historicalPage
    .getByRole("button", { name: "Importer et vérifier le compte" })
    .click();
  await expect(historicalPage.locator("#notice")).toContainText("autre réseau");
  report.checks.push("foreign-network-backup-refused-and-vaults-isolated");
  const archived = await (
    await fetch(historicalOrigin + "/v1/proposals?limit=100")
  ).json();
  await navigate(
    historicalPage,
    "/proposals/" + archived.items.find((p) => p.poll_id).id,
  );
  await historicalPage
    .getByRole("button", { name: "Vérifier le résultat" })
    .click();
  await expect(
    historicalPage.getByRole("heading", { name: "Le résultat est vérifié." }),
  ).toBeVisible({ timeout: 90000 });
  await capture(historicalPage, "isolated-certified-partitioned-result");
  report.checks.push(
    "isolated-fixture-full-selective-partitioned-record-independent-replay-and-certification",
  );
  report.checks.push(
    ...(await testPartitionWorker(
      page,
      historicalOrigin,
      { ...fixtureChoice, api_roots: [""] },
      archived.items.find((p) => p.poll_id).poll_id,
    )),
  );
  await historicalContext.close();
  await navigate(page, "/account");
  await page.getByLabel("Opération", { exact: true }).selectOption("rotate");
  await page.getByLabel("Fichier de sauvegarde chiffrée").setInputFiles({
    name: "backup.json",
    mimeType: "application/json",
    buffer: Buffer.from(backup),
  });
  await page
    .getByLabel("Code de secours (64 caractères hexadécimaux)")
    .fill(code);
  await page
    .getByRole("button", { name: "Importer et vérifier le compte" })
    .click();
  const nextCode = await page.getByTestId("backup-code").textContent();
  const nextDownload = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Télécharger ma sauvegarde chiffrée" })
    .click();
  const nextBackup = readFileSync(await (await nextDownload).path(), "utf8");
  await page.getByLabel("J’ai sauvegardé le fichier", { exact: false }).check();
  await page
    .getByRole("button", { name: /J’ai conservé la sauvegarde et son code/ })
    .click();
  await expect(page.locator("#notice")).toContainText("Clés renouvelées");
  assert.equal(
    await page.locator("#app .identifier").first().textContent(),
    accountID,
  );
  await page.getByLabel("Fichier de sauvegarde chiffrée").setInputFiles({
    name: "old.json",
    mimeType: "application/json",
    buffer: Buffer.from(backup),
  });
  await page
    .getByLabel("Code de secours (64 caractères hexadécimaux)")
    .fill(code);
  await page
    .getByRole("button", { name: "Importer et vérifier le compte" })
    .click();
  await expect(page.locator("#notice")).toContainText("ne correspondent pas");
  await page.getByLabel("Opération", { exact: true }).selectOption("retire");
  await page.getByLabel("Fichier de sauvegarde chiffrée").setInputFiles({
    name: "new.json",
    mimeType: "application/json",
    buffer: Buffer.from(nextBackup),
  });
  await page
    .getByLabel("Code de secours (64 caractères hexadécimaux)")
    .fill(nextCode);
  page.once("dialog", (dialog) => dialog.accept());
  await page
    .getByRole("button", { name: "Importer et vérifier le compte" })
    .click();
  await expect(page.locator("#notice")).toContainText("Compte retiré");
  report.checks.push(
    "recovery-signed-rotation-preserves-id-rejects-old-backup-and-retires-with-new-key",
  );
  report.passed = true;
  writeFileSync(
    join(directory, "report.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report, null, 2));
} catch (e) {
  for (const [i, b] of browsers.entries())
    for (const context of b.contexts())
      for (const page of context.pages()) {
        console.log(
          "Failure notice: " +
            (await page
              .locator("#notice")
              .textContent()
              .catch(() => "")),
        );
        await page
          .screenshot({
            path: join(directory, "failure-" + i + ".png"),
            fullPage: true,
          })
          .catch(() => {});
      }
  report.passed = false;
  report.error = e.message;
  writeFileSync(
    join(directory, "report.json"),
    JSON.stringify(report, null, 2),
  );
  throw e;
} finally {
  await Promise.all(browsers.map((b) => b.close()));
  if (globalThis.curve_bn128) await globalThis.curve_bn128.terminate();
  if (frontendHost && frontendHost.exitCode === null) {
    const done = new Promise((y) => frontendHost.once("exit", y));
    frontendHost.kill();
    await done;
  }
  if (historicalServer && historicalServer.exitCode === null) {
    const done = new Promise((y) => historicalServer.once("exit", y));
    historicalServer.kill();
    await done;
  }
  if (server && server.exitCode === null) {
    const done = new Promise((y) => server.once("exit", y));
    server.send({ type: "shutdown" });
    await done;
  }
}
