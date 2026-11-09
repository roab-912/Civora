import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import { createHash, randomUUID } from "node:crypto";
import {
  mkdirSync,
  readFileSync,
  writeFileSync,
  readdirSync,
  existsSync,
} from "node:fs";
import { resolve, join } from "node:path";
import { canonical, domain, signDigest } from "../dist/web/src/protocol.js";
import { buildParticipantBinaries } from "./build-participant.mjs";

process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
const { chromium, webkit, devices, expect } = await import("@playwright/test");
const directory = resolve(".tools/qualification/network-" + randomUUID());
mkdirSync(directory, { recursive: true });
const children = [],
  browsers = [],
  report = {
    mode: "LOCAL_TEST_ONLY",
    checks: [],
    passed: false,
    linux_runtime_tested: process.platform === "linux",
    directory,
  };
let server;
function execute(file, args = [], options = {}) {
  const result = spawnSync(file, args, {
    encoding: "utf8",
    windowsHide: true,
    ...options,
  });
  assert.equal(result.status, 0, result.error?.message ?? result.stderr);
  return result.stdout;
}
function connected(stdout) {
  const record = stdout
    .split(/\r?\n/)
    .filter((line) => line.startsWith("{"))
    .map((line) => JSON.parse(line))
    .at(-1);
  assert.equal(record?.connected, true, stdout);
  return record;
}

try {
  buildParticipantBinaries();
  server = spawn(process.execPath, ["scripts/serve-pwa.mjs", "--port", "0"], {
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe", "ipc"],
  });
  const origin = await new Promise((yes, no) => {
    let log = "";
    const timer = setTimeout(
      () => no(Error("Network server startup: " + log)),
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
    server.stdout.on("data", append);
    server.stderr.on("data", append);
    server.once("exit", () => {
      clearTimeout(timer);
      no(Error(log));
    });
    server.once("error", (error) => {
      clearTimeout(timer);
      no(error);
    });
  });
  const get = async (path) => {
    const response = await fetch(origin + path, {
      headers: { Connection: "close" },
    });
    assert(response.ok, await response.clone().text());
    return response.json();
  };
  const config = await get("/pwa-config.json");
  assert.equal(config.networks.length, 1);
  assert.equal((await fetch(origin + "/api/archive/v1/network")).status, 404);
  let overview = await get("/network/overview");
  assert.equal(overview.active_nodes, "1");
  assert.equal(overview.observer_nodes, "0");
  assert.equal(overview.citizen_accounts, "0");
  assert.equal(overview.test_accounts, "5");
  report.checks.push(
    "one-current-network-and-no-historical-archive-route-or-fake-human-count",
  );

  const platform =
    process.platform === "win32" ? "windows-amd64" : "linux-amd64";
  const download = await fetch(
    origin + "/network/download/" + platform + ".zip",
    { signal: AbortSignal.timeout(180000) },
  );
  if (!download.ok) throw Error(await download.text());
  const bytes = Buffer.from(await download.arrayBuffer());
  const digest = createHash("sha256").update(bytes).digest("hex");
  assert.equal(download.headers.get("x-content-sha256"), digest);
  const checksum = await get("/network/download/" + platform + ".sha256");
  assert.equal(checksum.sha256, digest);
  const archive = join(directory, "participant.zip"),
    extracted = join(directory, "extracted");
  writeFileSync(archive, bytes);
  if (process.platform === "win32")
    execute(
      "powershell.exe",
      [
        "-NoProfile",
        "-NonInteractive",
        "-Command",
        "Expand-Archive -LiteralPath $env:CIVORA_TEST_ZIP -DestinationPath $env:CIVORA_TEST_EXTRACT -ErrorAction Stop",
      ],
      {
        env: {
          ...process.env,
          CIVORA_TEST_ZIP: archive,
          CIVORA_TEST_EXTRACT: extracted,
        },
      },
    );
  else execute("unzip", ["-q", archive, "-d", extracted]);
  const exe = join(
    extracted,
    process.platform === "win32" ? "civora-reseau.exe" : "civora-reseau",
  );
  assert.deepEqual(
    readdirSync(extracted).sort(),
    [
      "LIRE-MOI.txt",
      "SHA256SUMS",
      process.platform === "win32" ? "civora-reseau.exe" : "civora-reseau",
      "reseau.json",
    ].sort(),
  );
  const bootstrap = JSON.parse(
    readFileSync(join(extracted, "reseau.json"), "utf8"),
  );
  assert.equal(bootstrap.network_id, config.networks[0].manifest.network_id);
  assert.equal(bootstrap.api_url, origin + "/api/atelier");
  assert.equal(bootstrap.public_test_only, true);
  assert.equal(bootstrap.ca_pem, "");
  assert(
    !readFileSync(join(extracted, "reseau.json"), "utf8").includes(
      "PRIVATE KEY",
    ),
  );
  report.native_archive_sha256 = digest;
  report.checks.push(
    "downloaded-native-zip-extracted-and-hash-checked-with-only-binary-public-bootstrap-and-guide",
  );

  // A native executable works with an empty PATH: neither Node nor Go is used.
  const home = join(directory, "node-a"),
    env = { ...process.env, PATH: directory };
  const first = connected(
    execute(exe, ["--home", home, "--once"], { env, cwd: directory }),
  );
  const again = connected(
    execute(exe, ["--home", home, "--once"], { env, cwd: directory }),
  );
  assert.equal(first.node_id, again.node_id);
  assert.equal(first.app_hash, again.app_hash);
  assert.equal((await get("/network/overview")).observer_nodes, "1");
  const secondHome = join(directory, "node-b");
  const second = connected(
    execute(exe, ["--home", secondHome, "--once"], { env }),
  );
  assert.notEqual(first.node_id, second.node_id);
  assert.equal((await get("/network/overview")).active_nodes, "3");
  report.checks.push(
    "native-one-command-join-without-sdk-real-replay-stable-identity-and-deduplicated-two-node-presence",
  );

  const child = spawn(exe, ["--home", home, "--interval", "2s"], {
    windowsHide: true,
    env,
    stdio: ["ignore", "pipe", "pipe"],
  });
  children.push(child);
  await new Promise((yes, no) => {
    let stdout = "",
      stderr = "";
    const timer = setTimeout(
      () => no(Error("Observer startup: " + stderr)),
      30000,
    );
    child.stdout.on("data", (data) => {
      stdout += data;
      if (stdout.includes('"connected":true')) {
        clearTimeout(timer);
        yes();
      }
    });
    child.stderr.on("data", (data) => {
      stderr += data;
    });
    child.once("exit", () => {
      clearTimeout(timer);
      no(Error(stderr));
    });
  });
  const crashed = new Promise((yes) => child.once("exit", yes));
  child.kill("SIGKILL");
  await crashed;
  const resumed = connected(execute(exe, ["--home", home, "--once"], { env }));
  assert.equal(resumed.node_id, first.node_id);
  assert.equal(resumed.app_hash, first.app_hash);
  assert.equal((await get("/network/overview")).observer_nodes, "2");
  report.checks.push(
    "downloaded-process-abrupt-kill-and-restart-retains-replayed-root-and-node-identity",
  );

  const tampered = join(directory, "foreign-bootstrap.json");
  writeFileSync(
    tampered,
    JSON.stringify({ ...bootstrap, network_id: "ff".repeat(32) }),
  );
  const untouched = join(directory, "must-not-create");
  const rejected = spawnSync(
    exe,
    ["--home", untouched, "--config", tampered, "--once"],
    { encoding: "utf8", windowsHide: true, env },
  );
  assert.equal(rejected.status, 1);
  assert(!existsSync(untouched));
  report.checks.push(
    "foreign-network-bootstrap-refused-before-creating-or-resetting-local-data",
  );

  for (const [node, nodeHome] of [
    [first, home],
    [second, secondHome],
  ]) {
    const challenge = await get("/network/challenge?node=" + node.node_id);
    const seed = JSON.parse(
      readFileSync(join(nodeHome, "presence-key.PRIVATE.json"), "utf8"),
    ).seed;
    const body = {
      format: "civora-presence-1",
      network_id: bootstrap.network_id,
      node_id: node.node_id,
      height: node.height,
      block_sha256: "00".repeat(32),
      challenge: challenge.challenge,
      action: "offline",
    };
    const response = await fetch(origin + "/network/presence", {
      method: "POST",
      body: canonical({
        body,
        signature: signDigest(
          domain("development/participant-presence", body),
          seed,
        ),
      }),
    });
    assert(response.ok);
  }
  assert.equal((await get("/network/overview")).active_nodes, "1");

  for (const [engine, options, name] of [
    [chromium, { viewport: { width: 1440, height: 1000 } }, "desktop"],
    [webkit, devices["iPhone 13"], "mobile-emulation"],
  ]) {
    const browser = await engine.launch({ headless: true });
    browsers.push(browser);
    const context = await browser.newContext(options),
      page = await context.newPage();
    await page.goto(origin);
    await expect(page.locator("#connection")).toHaveAttribute(
      "data-state",
      "verified",
    );
    await expect(page.locator("#application-nav")).toBeVisible();
    await expect(page.locator("#network-nav")).toBeHidden();
    assert.equal(await page.locator("select#network").count(), 0);
    assert(
      !(await page.locator("#connection").textContent()).includes("hauteur"),
    );
    await page
      .getByRole("navigation", { name: "Choisir un espace" })
      .getByRole("link", { name: "Réseau", exact: true })
      .click();
    await expect(page.locator('[data-network-stat="active_nodes"]')).toHaveText(
      "1",
    );
    await expect(page.locator("#application-nav")).toBeHidden();
    await expect(page.locator("#network-nav")).toBeVisible();
    await page.screenshot({
      path: join(directory, name + "-network.png"),
      fullPage: true,
    });
    await page
      .locator("#network-nav")
      .getByRole("link", { name: "Vérifier le réseau" })
      .click();
    await expect(
      page.getByRole("heading", { name: "Un téléchargement. Une commande." }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Télécharger pour Windows 64 bits" }),
    ).toHaveAttribute("href", "/network/download/windows-amd64.zip");
    await expect(
      page.getByRole("link", { name: "Télécharger pour Linux 64 bits" }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Copier la commande" }),
    ).toHaveCount(2);
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
      name + " horizontal overflow",
    );
    await page.screenshot({
      path: join(directory, name + "-listen.png"),
      fullPage: true,
    });
    await page
      .locator("#network-nav")
      .getByRole("link", { name: "Explorateur", exact: true })
      .click();
    await expect(page.locator(".explorer-blocks .card").first()).toBeVisible();
    await page.getByLabel("Numéro du bloc").fill("1");
    await page.getByRole("button", { name: "Consulter le bloc" }).click();
    await expect(page.locator('[data-block-verified="1"]')).toBeVisible();
    await page
      .getByRole("navigation", { name: "Contenu de l’explorateur" })
      .getByRole("link", { name: "États du registre" })
      .click();
    await expect(
      page.getByRole("heading", { name: "État vérifié", exact: true }),
    ).toBeVisible();
    await page.getByLabel("Type d’état").selectOption("accounts");
    await page.getByLabel("Identifiant public").fill("00".repeat(32));
    await page.getByRole("button", { name: "Consulter l’état" }).click();
    await expect(
      page.getByRole("heading", { name: "Absence vérifiée", exact: true }),
    ).toBeVisible();
    await page
      .locator("#network-nav")
      .getByRole("link", { name: "Vérifier un reçu" })
      .click();
    await expect(page.getByLabel("Fichier du reçu")).toBeVisible();
    let consensusPosts = 0;
    const countPosts = (request) => {
      if (request.method() === "POST") consensusPosts++;
    };
    page.on("request", countPosts);
    await page
      .locator("#network-nav")
      .getByRole("link", { name: "Participer au consensus" })
      .click();
    await expect(
      page.getByRole("heading", { name: "Participer au consensus." }),
    ).toBeVisible();
    await expect(page.getByRole("note")).toContainText(
      "Cette page est une maquette.",
    );
    await expect(
      page.getByRole("button", {
        name: "Participation au consensus indisponible",
      }),
    ).toBeDisabled();
    assert.equal(
      await page
        .locator(
          '#app a[download], #app a[href*="/network/download/"], #app input, #app form',
        )
        .count(),
      0,
    );
    assert.equal(consensusPosts, 0);
    page.off("request", countPosts);
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
      name + " consensus horizontal overflow",
    );
    await page.screenshot({
      path: join(directory, name + "-consensus-mock.png"),
      fullPage: true,
    });
    await page
      .getByRole("navigation", { name: "Choisir un espace" })
      .getByRole("link", { name: "Application", exact: true })
      .click();
    await expect(page.locator("#application-nav")).toBeVisible();
    await expect(page.locator("#network-nav")).toBeHidden();
    await context.close();
  }
  report.checks.push(
    "desktop-and-emulated-mobile-application-network-navigation-download-guide-and-no-horizontal-overflow",
    "observer-download-separated-from-static-consensus-mockup-with-disabled-actions-and-no-post",
    "read-only-explorer-authenticates-blocks-state-membership-and-state-absence",
  );
  report.passed = true;
} finally {
  await Promise.all(browsers.map((browser) => browser.close()));
  for (const child of children)
    if (child.exitCode === null && child.signalCode === null) {
      const done = new Promise((yes) => child.once("exit", yes));
      child.kill();
      await done;
    }
  if (server && server.exitCode === null) {
    const done = new Promise((yes) => server.once("exit", yes));
    server.send({ type: "shutdown" });
    await done;
  }
  writeFileSync(
    join(directory, "report.json"),
    JSON.stringify(report, null, 2),
  );
}
console.log(JSON.stringify(report, null, 2));
