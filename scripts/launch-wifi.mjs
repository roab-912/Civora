// Started by Civora-WiFi.exe, from the checkout next to that executable.
import { fork, spawn, spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { get } from "node:https";
import { createServer, isIP } from "node:net";
import { networkInterfaces } from "node:os";
import { dirname, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
process.chdir(root);
const input = createInterface({ input: process.stdin });
if (process.env.CIVORA_WIFI_MANAGED === "1")
  await new Promise((yes, no) => {
    input.once("line", (line) =>
      line === "START" ? yes() : no(Error("Demarrage interrompu")),
    );
    input.once("close", () => no(Error("Lanceur ferme")));
  });

const children = new Set();
const abort = new AbortController();
let stopping = false;
async function shutdown() {
  if (stopping) return;
  stopping = true;
  abort.abort();
  if (children.size)
    console.log("Arret des services demarres par ce lanceur...");
  const timer = setTimeout(() => process.exit(1), 10000);
  await Promise.all(
    [...children].map(
      (child) =>
        new Promise((done) => {
          if (child.exitCode !== null || child.signalCode !== null)
            return done();
          child.once("exit", done);
          // Disconnecting the workshop triggers its graceful shutdown, including
          // its ledger and notifications database. Other runners exit on SIGTERM.
          if (
            child.connected &&
            child.spawnargs.includes("scripts/serve-pwa.mjs")
          )
            child.disconnect();
          else child.kill();
        }),
    ),
  );
  clearTimeout(timer);
  input.close();
}
input.on("line", () => void shutdown());
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => void shutdown());

function command(args, ready) {
  abort.signal.throwIfAborted();
  return new Promise((yes, no) => {
    const child = fork(args[0], args.slice(1), {
      cwd: root,
      stdio: ["ignore", "pipe", "pipe", "ipc"],
      windowsHide: true,
    });
    children.add(child);
    let output = "",
      settled = false;
    const timer = setTimeout(() => {
      no(Error("Delai de demarrage depasse : " + args[0]));
      void shutdown();
    }, 240000);
    for (const [stream, target] of [
      [child.stdout, process.stdout],
      [child.stderr, process.stderr],
    ])
      stream.on("data", (data) => {
        target.write(data);
        output = (output + data.toString()).slice(-16000);
        if (ready?.test(output) && !settled) {
          settled = true;
          clearTimeout(timer);
          yes();
        }
      });
    child.once("error", (error) => {
      clearTimeout(timer);
      no(error);
    });
    child.once("exit", (code) => {
      clearTimeout(timer);
      children.delete(child);
      if (!settled) {
        if (code === 0 && !ready && !stopping) yes();
        else no(Error(`${args[0]} termine (code ${code}).`));
      } else if (!stopping) {
        console.error(`Service interrompu : ${args[0]} (code ${code}).`);
        process.exitCode = 1;
        void shutdown();
      }
    });
  });
}

async function portFree(host, port) {
  return new Promise((yes, no) => {
    const server = createServer();
    server.once("error", (error) => {
      if (error.code === "EADDRINUSE") yes(false);
      else no(error);
    });
    server.listen(port, host, () => server.close(() => yes(true)));
  });
}
function secureConfig(url) {
  return new Promise((yes, no) => {
    const request = get(
      url,
      { ca: readFileSync(".tools/pwa/lan-tls/ca.pem"), timeout: 5000 },
      (response) => {
        let body = "";
        response.setEncoding("utf8");
        response.on("data", (data) => {
          body += data;
          if (body.length > 1024 * 1024)
            request.destroy(Error("Reponse trop grande"));
        });
        response.on("error", no);
        response.on("end", () => {
          try {
            if (response.statusCode !== 200) throw Error("Pont indisponible");
            yes(JSON.parse(body));
          } catch (error) {
            no(error);
          }
        });
      },
    );
    request.on("error", no);
    request.on("timeout", () => request.destroy(Error("Pont sans reponse")));
  });
}

async function main() {
  const args = process.argv.slice(2);
  const options = {
    "--app-port": "8790",
    "--http-port": "8790",
    "--https-port": "8791",
  };
  const flags = new Set();
  for (let i = 0; i < args.length; i++) {
    const key = args[i];
    if (["--check", "--no-browser", "--help"].includes(key)) flags.add(key);
    else if (["--host", ...Object.keys(options)].includes(key) && args[i + 1])
      options[key] = args[++i];
    else throw Error("Option inconnue ou valeur manquante : " + key);
  }
  if (flags.has("--help")) {
    console.log(
      "Civora-WiFi.exe [--host IPv4] [--no-browser] [--check]\nPorts : --app-port 8790 --http-port 8790 --https-port 8791",
    );
    return;
  }
  const [major, minor] = process.versions.node.split(".").map(Number);
  if (major < 24 || (major === 24 && minor < 12))
    throw Error("Node.js >= 24.12 requis.");
  const adapters = Object.entries(networkInterfaces()).flatMap(
    ([name, addresses]) =>
      (addresses ?? [])
        .filter(
          (a) =>
            a.family === "IPv4" &&
            !a.internal &&
            /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(a.address),
        )
        .map((a) => ({ name, ...a })),
  );
  const wifi = adapters.filter((a) => /wi[ -]?fi|wlan|wireless/i.test(a.name));
  const physical = adapters.filter(
    (a) =>
      !/virtual|vethernet|wsl|vpn|docker|vmware|virtualbox|loopback/i.test(
        a.name,
      ),
  );
  const candidates = wifi.length ? wifi : physical;
  const host =
    options["--host"] ??
    (candidates.length === 1 ? candidates[0].address : undefined);
  if (isIP(host) !== 4 || !adapters.some((a) => a.address === host))
    throw Error(
      "Choisir une adresse avec --host IPv4 : " +
        adapters.map((a) => `${a.name} = ${a.address}`).join(", "),
    );
  const ports = ["--app-port", "--http-port", "--https-port"].map((key) =>
    Number(options[key]),
  );
  if (
    ports.some((p) => !Number.isInteger(p) || p < 1 || p > 65535) ||
    ports[1] === ports[2]
  )
    throw Error("Ports invalides.");
  const [appPort, httpPort, httpsPort] = ports;
  const upstream = `http://127.0.0.1:${appPort}`;
  const origin = `https://${host}:${httpsPort}`;
  const setup = `http://${host}:${httpPort}/__lan/`;
  console.log(`Civora - lancement Wi-Fi\nDossier : ${root}\nAdresse : ${host}`);
  if (flags.has("--check")) {
    console.log(
      `Node.js : ${process.versions.node}\nApplication : ${upstream}\nCertificat : ${setup}\nAcces Wi-Fi : ${origin}`,
    );
    return;
  }
  let config;
  const reuseApp = !(await portFree("127.0.0.1", appPort));
  if (reuseApp) {
    const response = await fetch(upstream + "/pwa-config.json", {
      signal: AbortSignal.timeout(5000),
      redirect: "error",
    });
    if (!response.ok)
      throw Error("Le port de l'application est occupe par un autre service.");
    config = await response.json();
    if (!config.development || !config.networks?.length)
      throw Error("Le serveur existant n'est pas un atelier Civora.");
    console.log("Atelier existant conserve.");
  }
  const bridgePorts = await Promise.all([
    portFree(host, httpPort),
    portFree(host, httpsPort),
  ]);
  const reuseBridge = bridgePorts.every((free) => !free);
  if (bridgePorts.some((free) => !free)) {
    if (!reuseApp || !reuseBridge)
      throw Error(
        "Un port du pont est occupe. Arreter l'ancien pont ou choisir d'autres ports.",
      );
    const bridged = await secureConfig(origin + "/pwa-config.json");
    if (
      !bridged.development ||
      JSON.stringify(bridged.networks) !== JSON.stringify(config.networks)
    )
      throw Error("Le pont existant ne correspond pas a cet atelier.");
    const response = await fetch(setup, {
      signal: AbortSignal.timeout(5000),
      redirect: "error",
    });
    if (!response.ok || !(await response.text()).includes(origin))
      throw Error("La page du certificat ne correspond pas au pont.");
    console.log("Pont Wi-Fi existant conserve.");
  }
  if (!reuseApp) {
    if (!existsSync("node_modules/typescript/bin/tsc"))
      throw Error(
        "Dependances absentes. Executer npm ci --ignore-scripts dans Civora.",
      );
    const go = existsSync(".tools/go/bin/go.exe")
      ? resolve(".tools/go/bin/go.exe")
      : "go";
    const version = spawnSync(go, ["version"], {
      encoding: "utf8",
      windowsHide: true,
    });
    if (version.error || version.status !== 0)
      throw Error("Go >= 1.26 requis (ou .tools/go/bin/go.exe).");
    console.log(
      "Verification des artefacts et preparation de l'application...",
    );
    for (const args of [
      ["scripts/fetch-semaphore.mjs"],
      ["scripts/generate-schemas.mjs", "--check"],
      ["node_modules/typescript/bin/tsc", "-p", "tsconfig.json"],
      ["scripts/build-pwa.mjs"],
      ["scripts/build-participant.mjs", "--build-participant"],
    ])
      await command(args);
    await command(
      ["scripts/serve-pwa.mjs", "--port", String(appPort)],
      /PUBLIC_TEST_ONLY_PWA http:/,
    );
  }
  if (!reuseBridge)
    await command(
      [
        "scripts/serve-pwa-lan.mjs",
        "--host",
        host,
        "--port",
        String(httpsPort),
        "--http-port",
        String(httpPort),
        "--upstream",
        upstream,
      ],
      /CIVORA_LAN https:/,
    );
  abort.signal.throwIfAborted();
  console.log(
    `\nCIVORA_WIFI_READY\nSur ce PC : ${upstream}\nSur l'autre appareil : ${setup}\nApres installation du certificat : ${origin}\n\nGarder cette fenetre ouverte. Entree ou Ctrl+C pour arreter.\nLes services deja presents restent actifs apres fermeture.`,
  );
  if (!flags.has("--no-browser")) {
    const browser = spawn("explorer.exe", [setup], {
      windowsHide: true,
      stdio: "ignore",
    });
    browser.on("error", () => console.log("Ouvrir manuellement : " + setup));
    browser.unref();
  }
  await new Promise((done) => {
    if (stopping) return done();
    abort.signal.addEventListener("abort", done, { once: true });
  });
}

try {
  await main();
} catch (error) {
  if (!stopping) {
    console.error("Erreur : " + error.message);
    process.exitCode = 1;
  }
} finally {
  await shutdown();
}
