// Local Wi-Fi development bridge. The existing loopback application keeps its
// ledger/session. A private local CA is generated under .tools, never published.
import { createServer as httpServer, request } from "node:http";
import { createServer as httpsServer } from "node:https";
import { networkInterfaces } from "node:os";
import { isIP } from "node:net";
import { X509Certificate } from "node:crypto";
import { spawnSync } from "node:child_process";
import { readFileSync, writeFileSync, existsSync, mkdirSync } from "node:fs";
import { resolve, join } from "node:path";
const args = process.argv.slice(2),
  get = (key, fallback) => {
    const at = args.indexOf(key);
    return at < 0 ? fallback : args[at + 1];
  };
const host = get("--host"),
  tlsPort = Number(get("--port", "8791")),
  httpPort = Number(get("--http-port", "8790")),
  upstream = new URL(get("--upstream", "http://127.0.0.1:8790")),
  directory = resolve(get("--cert-dir", ".tools/pwa/lan-tls"));
if (
  isIP(host) !== 4 ||
  !/^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(host)
)
  throw Error("Pass an explicit private IPv4 address with --host");
const adapter = Object.values(networkInterfaces())
  .flat()
  .find((a) => a?.family === "IPv4" && !a.internal && a.address === host);
if (!adapter) throw Error("--host must be an address of this PC");
if (
  upstream.protocol !== "http:" ||
  upstream.hostname !== "127.0.0.1" ||
  upstream.username ||
  upstream.password ||
  upstream.pathname !== "/" ||
  upstream.search ||
  upstream.hash
)
  throw Error("Upstream must be the existing loopback HTTP server");
for (const port of [tlsPort, httpPort])
  if (!Number.isInteger(port) || port < 1 || port > 65535)
    throw Error("Invalid port");
if (tlsPort === httpPort) throw Error("HTTP and HTTPS require distinct ports");
mkdirSync(directory, { recursive: true, mode: 0o700 });
const file = (n) => join(directory, n),
  openssl = get(
    "--openssl",
    process.env.OPENSSL ??
      (existsSync("C:/Program Files/Git/mingw64/bin/openssl.exe")
        ? "C:/Program Files/Git/mingw64/bin/openssl.exe"
        : "openssl"),
  );
function run(...argv) {
  const r = spawnSync(openssl, argv, { encoding: "utf8", windowsHide: true });
  if (r.status !== 0)
    throw Error("OpenSSL: " + (r.error?.message ?? r.stderr).slice(0, 2000));
}
if (!existsSync(file("ca.pem")))
  run(
    "req",
    "-x509",
    "-newkey",
    "rsa:2048",
    "-nodes",
    "-sha256",
    "-days",
    "365",
    "-keyout",
    file("ca-key.pem"),
    "-out",
    file("ca.pem"),
    "-subj",
    "/CN=Civora local Wi-Fi development CA",
    "-addext",
    "basicConstraints=critical,CA:TRUE",
    "-addext",
    "keyUsage=critical,keyCertSign,cRLSign",
  );
const ca = new X509Certificate(readFileSync(file("ca.pem")));
if (!ca.ca || Date.parse(ca.validTo) <= Date.now() + 86400000)
  throw Error(
    "Local CA expired; renew it and approve its replacement on clients",
  );
let leaf;
try {
  leaf = new X509Certificate(readFileSync(file("server.pem")));
} catch {}
if (
  !leaf ||
  !leaf.checkIP(host) ||
  !leaf.verify(ca.publicKey) ||
  Date.parse(leaf.validTo) <= Date.now() + 86400000
) {
  writeFileSync(
    file("server.ext"),
    `basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\nsubjectAltName=IP:${host}\n`,
    { mode: 0o600 },
  );
  run(
    "req",
    "-new",
    "-newkey",
    "rsa:2048",
    "-nodes",
    "-keyout",
    file("server-key.pem"),
    "-out",
    file("server.csr"),
    "-subj",
    "/CN=Civora Wi-Fi development",
  );
  run(
    "x509",
    "-req",
    "-in",
    file("server.csr"),
    "-CA",
    file("ca.pem"),
    "-CAkey",
    file("ca-key.pem"),
    "-CAcreateserial",
    "-out",
    file("server.pem"),
    "-days",
    "30",
    "-sha256",
    "-extfile",
    file("server.ext"),
  );
}
writeFileSync(file("civora-local-ca.cer"), ca.raw);
const origin = `https://${host}:${tlsPort}`,
  httpOrigin = `http://${host}:${httpPort}`,
  integer = (ip) => ip.split(".").reduce((n, p) => (n << 8) | Number(p), 0),
  mask = integer(adapter.netmask),
  allowed = (req) =>
    isIP(req.socket.remoteAddress) === 4 &&
    (integer(req.socket.remoteAddress) & mask) === (integer(host) & mask);
const certificate = (req, res) => {
  if (!["GET", "HEAD"].includes(req.method)) {
    res.writeHead(405);
    res.end();
    return;
  }
  res.writeHead(200, {
    "Content-Type": "application/pkix-cert",
    "Content-Disposition": 'attachment; filename="civora-local-ca.cer"',
    "Cache-Control": "no-store",
  });
  res.end(req.method === "HEAD" ? undefined : ca.raw);
};
const onboarding = httpServer((req, res) => {
  if (!allowed(req) || req.headers.host !== `${host}:${httpPort}`) {
    res.writeHead(403);
    res.end();
    return;
  }
  const url = new URL(req.url, httpOrigin);
  if (url.pathname === "/__lan/ca.cer") {
    certificate(req, res);
    return;
  }
  if (!["GET", "HEAD"].includes(req.method)) {
    res.writeHead(426);
    res.end("Use HTTPS for account and ballot operations");
    return;
  }
  if (url.pathname !== "/" && url.pathname !== "/__lan/") {
    res.writeHead(307, { Location: origin + url.pathname + url.search });
    res.end();
    return;
  }
  res.writeHead(200, {
    "Content-Type": "text/html; charset=utf-8",
    "Cache-Control": "no-store",
    "Content-Security-Policy":
      "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
  });
  res.end(
    `<!doctype html><html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Civora — accès Wi-Fi</title><h1>Ouvrir Civora en HTTPS</h1><p>Le coffre chiffré et le vote nécessitent une connexion HTTPS approuvée par votre navigateur.</p><ol><li><a href="/__lan/ca.cer">Télécharger le certificat local Civora</a>.</li><li>Sur Windows : ouvrir le fichier, choisir « Installer le certificat », « Utilisateur actuel », puis le magasin « Autorités de certification racines de confiance ». Pour Firefox, l’importer dans les autorités de son gestionnaire de certificats si nécessaire.</li><li>Fermer puis rouvrir le navigateur.</li><li><a href="${origin}/">Ouvrir Civora : ${origin}</a>.</li></ol><p>Ce certificat est propre à cet atelier local. Empreinte SHA-256 de l’autorité : <code>${ca.fingerprint256}</code>.</p><p>Les comptes sont propres à chaque origine : pour retrouver un compte créé sur une autre adresse, importer sa sauvegarde chiffrée et son code.</p></html>`,
  );
});
const secure = httpsServer(
  {
    key: readFileSync(file("server-key.pem")),
    cert: readFileSync(file("server.pem")),
    minVersion: "TLSv1.2",
  },
  (req, res) => {
    if (!allowed(req) || req.headers.host !== `${host}:${tlsPort}`) {
      res.writeHead(403);
      res.end();
      return;
    }
    const url = new URL(req.url, origin);
    if (url.pathname === "/__lan/ca.cer") {
      certificate(req, res);
      return;
    }
    if (req.headers.origin && req.headers.origin !== origin) {
      res.writeHead(403);
      res.end();
      return;
    }
    const headers = { ...req.headers, host: upstream.host };
    if (req.headers.origin) headers.origin = upstream.origin;
    const forward = request(
      {
        hostname: upstream.hostname,
        port: upstream.port || 80,
        path: url.pathname + url.search,
        method: req.method,
        headers,
        timeout: 30000,
      },
      (response) => {
        res.writeHead(response.statusCode, response.headers);
        response.pipe(res);
        response.on("error", () => res.destroy());
      },
    );
    forward.on("timeout", () => forward.destroy(Error("timeout")));
    forward.on("error", () => {
      if (!res.headersSent) res.writeHead(502);
      res.end("Local application unavailable");
    });
    req.on("aborted", () => forward.destroy());
    res.on("close", () => forward.destroy());
    req.pipe(forward);
  },
);
for (const [server, port] of [
  [onboarding, httpPort],
  [secure, tlsPort],
]) {
  server.on("error", (error) => {
    console.error(error.message);
    process.exit(1);
  });
  await new Promise((y) => server.listen(port, host, y));
}
console.log(
  `CIVORA_LAN ${origin}\nCertificate setup: ${httpOrigin}/__lan/\nCA SHA-256: ${ca.fingerprint256}\nExisting session: ${upstream.origin}`,
);
function close() {
  onboarding.closeAllConnections();
  secure.closeAllConnections();
  onboarding.close();
  secure.close();
}
process.on("SIGINT", close);
process.on("SIGTERM", close);
