// Real browser engines with mobile emulation, not physical device qualification.
import { createServer } from "node:http";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { spawnSync, spawn } from "node:child_process";
import { build } from "esbuild";
import { canonical } from "../dist/web/src/codec.js";
process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
const { chromium, webkit, devices } = await import("@playwright/test");
await build({
  entryPoints: ["web/src/eligibility-worker.ts"],
  outfile: ".tools/browser/eligibility-worker.js",
  bundle: true,
  platform: "browser",
  format: "esm",
});
mkdirSync(".tools/bin", { recursive: true });
mkdirSync(".tools/qualification", { recursive: true });
const binary = resolve(
  ".tools/bin/civeligibility" + (process.platform === "win32" ? ".exe" : ""),
);
const go = existsSync(".tools/go/bin/go.exe")
  ? resolve(".tools/go/bin/go.exe")
  : "go";
const env = {
  ...process.env,
  GOPATH: resolve(".tools/gopath"),
  GOMODCACHE: resolve(".tools/gomodcache"),
  GOCACHE: resolve(".tools/gocache"),
};
const compilation = spawnSync(
  go,
  ["build", "-buildvcs=false", "-o", binary, "./cmd/civeligibility"],
  { env, encoding: "utf8" },
);
if (compilation.status !== 0) throw Error(compilation.stderr);
let corrupt = false;
const requests = [];
const server = createServer((req, res) => {
  requests.push(req.url);
  res.setHeader("Cache-Control", "no-store");
  if (req.url === "/") {
    res.setHeader("Content-Type", "text/html");
    res.end(`<!doctype html><meta charset="utf-8"><title>Semaphore worker test</title><script>
    window.run = async input => {
      const worker = new Worker('/eligibility-worker.js',{type:'module'});
      let sequence=0; const pending=new Map();
      worker.onmessage=e=>{const p=pending.get(e.data.id);if(!p)return;pending.delete(e.data.id);e.data.error?p.reject(new Error(e.data.error)):p.resolve(e.data.result);};
      worker.onerror=e=>{for(const p of pending.values())p.reject(new Error(e.message));};
      const call = value=>new Promise((resolve,reject)=>{const id=String(++sequence);pending.set(id,{resolve,reject});worker.postMessage({...value,id});});
      let ticks=0;const timer=setInterval(()=>ticks++,10);
      try { const identity=await call({type:'import',identity:input.identity});
        const bytes=Uint8Array.from(atob(input.ballot.replace(/-/g,'+').replace(/_/g,'/')),c=>c.charCodeAt(0));
        const path=input.path_url?await (await fetch(input.path_url,{credentials:'omit',cache:'no-store'})).json():input.path;
        const result=await call({type:'prove',path,expected:input.expected,context:input.context,ballot:bytes});return {...result,commitment:identity.commitment,main_thread_ticks:ticks};
      }finally{clearInterval(timer);worker.terminate();}
    };
  </script>`);
    return;
  }
  if (req.url === "/eligibility-worker.js") {
    res.setHeader("Content-Type", "text/javascript");
    res.end(readFileSync(".tools/browser/eligibility-worker.js"));
    return;
  }
  const match =
    /^\/crypto\/semaphore\/(16|26)\/(semaphore\.(wasm|zkey)|verification-key\.json)$/.exec(
      req.url ?? "",
    );
  if (match) {
    const depth = match[1],
      name = match[2];
    const path =
      name === "verification-key.json"
        ? `spec/crypto/semaphore-${depth}.verification-key.json`
        : `.tools/semaphore/${depth}/${name}`;
    const bytes = Buffer.from(readFileSync(path));
    if (corrupt && name === "semaphore.wasm") bytes[0] ^= 1;
    res.setHeader(
      "Content-Type",
      name.endsWith(".wasm") ? "application/wasm" : "application/octet-stream",
    );
    res.end(bytes);
    return;
  }
  res.writeHead(404);
  res.end();
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const address = server.address();
const origin = `http://127.0.0.1:${address.port}`;
const evidence = {
  physical_devices: false,
  mobile_emulation: true,
  browsers: [],
};
let pathServer, scaleCase;
try {
  if (process.env.CIVORA_SCALE_STORE) {
    const store = resolve(process.env.CIVORA_SCALE_STORE);
    const local = spawnSync(
      binary,
      ["path", "-store", store, "-height", "1", "-index", "9999999"],
      { encoding: "utf8" },
    );
    if (local.status !== 0) throw Error(local.stderr);
    const path = JSON.parse(local.stdout),
      fixture = JSON.parse(
        readFileSync("spec/fixtures/v0.3/membership-26.json", "utf8"),
      );
    if (
      path.snapshot.size !== "10000000" ||
      path.snapshot.active_count !== "10000000"
    )
      throw Error("Expected ten million active leaves");
    fixture.path = path;
    fixture.context.group_root = path.snapshot.root;
    pathServer = spawn(
      binary,
      ["serve", "-store", store, "-addr", "127.0.0.1:0"],
      { stdio: ["ignore", "ignore", "pipe"] },
    );
    const apiOrigin = await new Promise((resolve, reject) => {
      let stderr = "";
      const timer = setTimeout(
        () => reject(Error("Path API startup timeout")),
        20000,
      );
      pathServer.stderr.on("data", (chunk) => {
        stderr += chunk;
        const match =
          /Development path API: (http:\/\/127\.0\.0\.1:[0-9]+)/.exec(stderr);
        if (match) {
          clearTimeout(timer);
          resolve(match[1]);
        }
      });
      pathServer.on("error", reject);
      pathServer.on("exit", (code) => {
        clearTimeout(timer);
        reject(Error("Path API exited " + code + ": " + stderr));
      });
    });
    scaleCase = {
      label: "scale-10m",
      depth: "26",
      fixture,
      path_url: apiOrigin + "/v1/eligibility/snapshots/1/paths/9999999",
    };
  }
  for (const [name, engine, device] of [
    ["Chromium", chromium, devices["Pixel 7"]],
    ["WebKit", webkit, devices["iPhone 13"]],
  ]) {
    const browser = await engine.launch({ headless: true });
    try {
      const context = await browser.newContext({ ...device });
      const page = await context.newPage();
      await page.goto(origin);
      const result = {
        name,
        version: browser.version(),
        emulated_device: device.userAgent,
        proofs: [],
      };
      const cases = ["16", "26"].map((depth) => ({
        label: depth,
        depth,
        fixture: JSON.parse(
          readFileSync(`spec/fixtures/v0.3/membership-${depth}.json`, "utf8"),
        ),
      }));
      if (scaleCase) cases.push(scaleCase);
      for (const entry of cases) {
        const { depth, fixture, label } = entry;
        const input = {
          identity: fixture.identity_export_TEST_ONLY,
          path: fixture.path,
          expected: fixture.path.snapshot,
          context: fixture.context,
          ballot: fixture.ballot_base64url,
          ...(entry.path_url ? { path_url: entry.path_url } : {}),
        };
        const started = Date.now();
        const output = await page.evaluate((input) => window.run(input), input);
        if (
          output.commitment !== fixture.path.leaf ||
          output.membership.nullifier !== fixture.membership.nullifier ||
          output.main_thread_ticks < 1
        )
          throw Error("Worker isolation or identity/nullifier mismatch");
        const proofPath = resolve(`.tools/qualification/${name}-${label}.json`);
        writeFileSync(
          proofPath,
          canonical({ ...fixture, membership: output.membership }),
        );
        const verification = spawnSync(
          binary,
          ["verify-membership", "-input", proofPath],
          { encoding: "utf8" },
        );
        if (verification.status !== 0) throw Error(verification.stderr);
        result.proofs.push({
          depth,
          leaves: fixture.path.snapshot.size,
          path_api: !!entry.path_url,
          elapsed_ms: String(Date.now() - started),
          main_thread_ticks: String(output.main_thread_ticks),
          go_verified: true,
        });
        const broken = structuredClone(input);
        delete broken.path_url;
        broken.path.siblings[0] = (
          BigInt(broken.path.siblings[0]) + 1n
        ).toString();
        let refused = false;
        try {
          await page.evaluate((input) => window.run(input), broken);
        } catch (e) {
          refused = String(e).includes("ERR_PATH");
        }
        if (!refused) throw Error("Forged Merkle path was accepted");
      }
      corrupt = true;
      try {
        const fixture = JSON.parse(
          readFileSync("spec/fixtures/v0.3/membership-16.json", "utf8"),
        );
        let refused = false;
        try {
          await page.evaluate((input) => window.run(input), {
            identity: fixture.identity_export_TEST_ONLY,
            path: fixture.path,
            expected: fixture.path.snapshot,
            context: fixture.context,
            ballot: fixture.ballot_base64url,
          });
        } catch (e) {
          refused = String(e).includes("ERR_ARTIFACT_HASH");
        }
        if (!refused) throw Error("Corrupted circuit artifact was accepted");
      } finally {
        corrupt = false;
      }
      evidence.browsers.push(result);
      console.log(
        JSON.stringify({
          browser: name,
          result: "passed",
          proofs: result.proofs,
        }),
      );
      await context.close();
    } finally {
      await browser.close();
    }
  }
  if (
    requests.some((url) => url.includes("members") || url.includes("identity"))
  )
    throw Error("Browser fetched an identity list");
  writeFileSync(
    ".tools/qualification/browser-v0.3.json",
    JSON.stringify(evidence, null, 2) + "\n",
  );
} finally {
  if (
    pathServer &&
    pathServer.exitCode === null &&
    pathServer.signalCode === null
  ) {
    pathServer.kill();
    await Promise.race([
      new Promise((resolve) => pathServer.once("exit", resolve)),
      new Promise((resolve) => setTimeout(resolve, 2000)),
    ]);
  }
  server.closeAllConnections();
  await new Promise((resolve) => server.close(resolve));
}
