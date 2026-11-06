// Real finalised Go openings and real browser service workers. The browser
// subscription and provider are explicitly TEST_ONLY; no external Push is sent.
import { spawn } from "node:child_process";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { resolve, join } from "node:path";
import assert from "node:assert/strict";
process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
const {
    chromium,
    webkit,
    devices,
    expect: baseExpect,
  } = await import("@playwright/test"),
  expect = baseExpect.configure({ timeout: 25000 }),
  directory = resolve(".tools/qualification/notifications-" + Date.now());
mkdirSync(directory, { recursive: true });
const report = {
    milestone: "v0.8",
    mode: "PUBLIC_TEST_ONLY_PROVIDER_AND_BROWSER_SUBSCRIPTION_REAL_FINALITY_AND_SW",
    physical_ios: false,
    checks: [],
  },
  server = spawn(
    process.execPath,
    ["scripts/serve-pwa.mjs", "--port", "0", "--notifications-test-only"],
    { windowsHide: true, stdio: ["ignore", "pipe", "pipe", "ipc"] },
  );
let browser, mobile;
try {
  let log = "";
  const origin = await new Promise((yes, no) => {
    const timer = setTimeout(() => no(Error("startup: " + log)), 120000);
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
    server.once("exit", () => {
      clearTimeout(timer);
      no(Error(log));
    });
  });
  browser = await chromium.launch({ headless: true, channel: "chromium" });
  const context = await browser.newContext({
      permissions: ["notifications"],
      acceptDownloads: true,
    }),
    vector = JSON.parse(
      readFileSync("spec/fixtures/v0.8/webpush-rfc8291.TEST_ONLY.json", "utf8"),
    );
  await context.addInitScript(
    (keys) => {
      window.__permissionRequests = 0;
      let current = null;
      Object.defineProperty(Notification, "permission", {
        configurable: true,
        get: () => "default",
      });
      Notification.requestPermission = async () => {
        window.__permissionRequests++;
        return "granted";
      };
      PushManager.prototype.getSubscription = async () => current;
      PushManager.prototype.subscribe = async (options) =>
        (current = {
          endpoint: "https://push.example.test/PUBLIC_TEST_ONLY",
          options,
          async unsubscribe() {
            current = null;
            return true;
          },
          toJSON() {
            return { endpoint: this.endpoint, keys };
          },
        });
    },
    { p256dh: vector.receiver_public, auth: vector.auth },
  );
  await context.grantPermissions(["notifications"], { origin });
  const page = await context.newPage();
  await page.goto(origin);
  await page.getByRole("link", { name: "Actualités", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Les actualités", exact: true }),
  ).toBeVisible();
  assert.equal(await page.evaluate(() => window.__permissionRequests), 0);
  await page
    .getByRole("button", { name: "Activer les notifications", exact: true })
    .click();
  await expect(page.locator("#notice")).toContainText("Notifications activées");
  await expect(page.locator("body")).not.toHaveAttribute("aria-busy", "true");
  assert.equal(await page.evaluate(() => window.__permissionRequests), 1);
  report.checks.push(
    "consent-is-explicit-and-subscription-needs-no-civic-account",
  );
  // Inspect the actual outbound subscription body without recording endpoints,
  // auth secrets or browser/voter activity.
  const metadata = await page.evaluate(
    () =>
      new Promise((yes, no) => {
        const req = indexedDB.open("civora-pwa-1");
        req.onsuccess = () => {
          const get = req.result
            .transaction("settings")
            .objectStore("settings")
            .get("notifications");
          get.onsuccess = () => yes(Object.keys(get.result));
          get.onerror = no;
        };
      }),
  );
  assert(!metadata.includes("account_id"));
  await page.goto(origin + "/#/account");
  await expect(page.locator("body")).not.toHaveAttribute("aria-busy", "true");
  await expect(page.getByLabel("Choisir le réseau")).toBeEnabled();
  await page
    .getByRole("button", { name: "Créer un compte", exact: true })
    .click();
  await expect(page.getByTestId("backup-code")).toBeVisible();
  await expect(page.locator("body")).not.toHaveAttribute("aria-busy", "true");
  await Promise.all([
    page.waitForEvent("download"),
    page
      .getByRole("button", { name: "Télécharger ma sauvegarde chiffrée" })
      .click(),
  ]);
  await page.getByLabel("J’ai sauvegardé le fichier", { exact: false }).check();
  await page
    .getByRole("button", { name: /J’ai conservé la sauvegarde et son code/ })
    .click();
  await expect(page.locator("#notice")).toContainText("Compte inscrit");
  async function open(title) {
    await expect(page.locator("body")).not.toHaveAttribute("aria-busy", "true");
    await page.goto(origin + "/#/new/law.create");
    await page.getByLabel("Titre du texte", { exact: true }).fill(title);
    await page
      .getByLabel("Texte intégral proposé", { exact: true })
      .fill(
        "Article 1. Une annonce d’ouverture est consultable sans activer les notifications.",
      );
    await page
      .getByLabel("Pourquoi proposer cette décision ?", { exact: true })
      .fill("PUBLIC_TEST_ONLY : vérifier la finalité et les annonces.");
    await page.getByRole("button", { name: "Relire avant de déposer" }).click();
    await page
      .getByRole("button", { name: "Confirmer le dépôt public" })
      .click();
    await page
      .getByRole("button", { name: "Atelier : ouvrir le vote" })
      .click();
    await expect(
      page.getByRole("button", { name: "Participer au vote" }),
    ).toBeVisible();
  }
  await open("Premier scrutin de notification TEST_ONLY");
  await page.goto(origin + "/#/news");
  await expect(
    page.getByRole("heading", { name: "Un référendum a été ouvert" }),
  ).toBeVisible();
  const service = await (await fetch(origin + "/__dev/notifications")).json();
  assert.equal(service.messages.length, 1);
  const message = service.messages[0];
  assert.deepEqual(
    Object.keys(message).sort(),
    ["v", "type", "event_id", "network_id", "poll_id", "closes_at_ms"].sort(),
  );
  report.checks.push(
    "native-finalised-opening-creates-one-outbox-delivery-and-public-news",
  );
  const worker = context.serviceWorkers()[0];
  assert(worker, "worker registration missing");
  assert.equal(await worker.evaluate(() => Notification.permission), "granted");
  const push = (msg) =>
    worker.evaluate(async (message) => {
      const work = [],
        event = new PushEvent("push", { data: JSON.stringify(message) });
      // A fabricated event has no browser lifecycle; capture waitUntil promises
      // while exercising the installed handler and real Notification API.
      Object.defineProperty(event, "waitUntil", {
        value: (promise) => work.push(promise),
      });
      self.dispatchEvent(event);
      await Promise.all(work);
    }, msg);
  await push(message);
  await expect
    .poll(() =>
      page.evaluate(() =>
        navigator.serviceWorker.ready.then(
          async (r) => (await r.getNotifications()).length,
        ),
      ),
    )
    .toBe(1);
  const notification = await page.evaluate(() =>
    navigator.serviceWorker.ready.then(async (r) => {
      const n = (await r.getNotifications())[0];
      return { title: n.title, body: n.body, data: n.data };
    }),
  );
  assert.equal(notification.title, "Nouveau référendum");
  assert.equal(notification.body, "Consulter la question");
  await push(message);
  await page.waitForTimeout(250);
  assert.equal(
    await page.evaluate(() =>
      navigator.serviceWorker.ready.then(
        async (r) => (await r.getNotifications()).length,
      ),
    ),
    1,
  );
  await push({ ...message, event_id: "aa".repeat(32), closes_at_ms: "1" });
  await push({
    ...message,
    event_id: "bb".repeat(32),
    network_id: "cc".repeat(32),
  });
  await push({ ...message, event_id: "dd".repeat(32), choice: "yes" });
  await page.waitForTimeout(300);
  assert.equal(
    await page.evaluate(() =>
      navigator.serviceWorker.ready.then(
        async (r) => (await r.getNotifications()).length,
      ),
    ),
    1,
  );
  report.checks.push(
    "real-service-worker-handles-test-PushEvent-with-generic-notification-and-refuses-duplicates-expired-foreign-and-extra-fields",
  );
  await page.goto(
    origin + "/#/polls/" + message.poll_id + "?network=" + message.network_id,
  );
  await expect(
    page.getByRole("heading", {
      name: "Premier scrutin de notification TEST_ONLY",
      exact: true,
      level: 1,
    }),
  ).toBeVisible();
  report.checks.push(
    "notification-deep-link-opens-the-authenticated-question-without-casting-a-vote",
  );
  await page.goto(origin + "/#/news");
  await page
    .getByRole("button", { name: "Désactiver et me désabonner" })
    .click();
  await expect(page.locator("#notice")).toContainText("Abonnement supprimé");
  await open("Deuxième scrutin après désabonnement TEST_ONLY");
  await page.goto(origin + "/#/news");
  await expect(
    page.getByRole("heading", { name: "Un référendum a été ouvert" }),
  ).toHaveCount(2);
  assert.equal(
    (await (await fetch(origin + "/__dev/notifications")).json()).messages
      .length,
    1,
  );
  report.checks.push("unsubscribe-stops-future-push-without-removing-news");
  const denied = await browser.newContext();
  await denied.addInitScript(() => {
    window.__permissionRequests = 0;
    Object.defineProperty(Notification, "permission", {
      configurable: true,
      get: () => "default",
    });
    Notification.requestPermission = async () => {
      window.__permissionRequests++;
      return "denied";
    };
  });
  const refusal = await denied.newPage();
  await refusal.goto(origin + "/#/news");
  await expect(
    refusal.getByRole("heading", { name: "Un référendum a été ouvert" }),
  ).toHaveCount(2);
  assert.equal(await refusal.evaluate(() => window.__permissionRequests), 0);
  await refusal
    .getByRole("button", { name: "Activer les notifications", exact: true })
    .click();
  await expect(refusal.locator("#notice")).toContainText(
    "ne sont pas autorisées",
  );
  await expect(
    refusal.getByRole("heading", { name: "Un référendum a été ouvert" }),
  ).toHaveCount(2);
  report.checks.push("permission-refusal-leaves-public-inbox-accessible");
  await page.evaluate(() => navigator.serviceWorker.ready);
  await page.reload();
  await context.setOffline(true);
  await page.reload();
  await expect(
    page.getByText("Actualités en cache", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Un référendum a été ouvert" }),
  ).toHaveCount(2);
  await context.setOffline(false);
  report.checks.push("offline-public-news-cache-remains-readable");
  mobile = await webkit.launch({ headless: true });
  const ios = await mobile.newContext({ ...devices["iPhone 13"] }),
    phone = await ios.newPage();
  await phone.goto(origin + "/#/news");
  await expect(
    phone.getByRole("heading", { name: "Les actualités", exact: true }),
  ).toBeVisible();
  await expect(
    phone.getByText("Sur iPhone ou iPad, ouvrez Partager", { exact: false }),
  ).toBeVisible();
  await expect(
    phone.getByRole("heading", { name: "Un référendum a été ouvert" }),
  ).toHaveCount(2);
  report.checks.push(
    "WebKit-iPhone-emulation-explains-home-screen-installation-and-keeps-news-readable",
  );
  await phone.screenshot({
    path: join(directory, "ios-news.png"),
    fullPage: true,
  });
  await page.screenshot({
    path: join(directory, "desktop-news.png"),
    fullPage: true,
  });
  report.passed = true;
  writeFileSync(
    join(directory, "report.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify({ ...report, directory }, null, 2));
} catch (e) {
  if (browser)
    for (const c of browser.contexts())
      for (const p of c.pages())
        console.log(
          "Failure view:",
          await p
            .locator("#app h1")
            .textContent()
            .catch(() => ""),
          await p
            .locator("#notice")
            .textContent()
            .catch(() => ""),
        );
  report.passed = false;
  report.error = e.message;
  writeFileSync(
    join(directory, "report.json"),
    JSON.stringify(report, null, 2),
  );
  throw e;
} finally {
  await browser?.close();
  await mobile?.close();
  if (server.exitCode === null) {
    const exited = new Promise((y) => server.once("exit", y));
    server.send({ type: "shutdown" });
    await exited;
  }
}
