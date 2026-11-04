/// <reference lib="webworker" />
import { Identity } from "@semaphore-protocol/identity";
import { generateProof } from "@semaphore-protocol/proof";
import { verifyMembership } from "./semaphore.js";
import { semaphoreLock } from "./semaphore-lock.js";
import {
  binding,
  checkContext,
  sha256,
  verifyPath,
  type EligibilityPath,
  type Snapshot,
  type MembershipContext,
  type Membership,
} from "./eligibility.js";

let identity: Identity | undefined;
const artifacts = new Map<
  string,
  Promise<{ wasm: Uint8Array; zkey: Uint8Array }>
>();
async function load(
  depth: "16" | "26",
): Promise<{ wasm: Uint8Array; zkey: Uint8Array }> {
  const record = semaphoreLock.depths.find((v) => v.depth === depth);
  if (!record) throw Error("ERR_CIRCUIT_DEPTH");
  const read = async (
    kind: "wasm" | "zkey" | "verification_key",
  ): Promise<Uint8Array> => {
    const info = record[kind];
    const name =
      kind === "verification_key"
        ? "verification-key.json"
        : "semaphore." + kind;
    const response = await fetch("/crypto/semaphore/" + depth + "/" + name, {
      credentials: "omit",
      cache: "no-store",
    });
    if (!response.ok) throw Error("ERR_ARTIFACT_FETCH");
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (
      String(bytes.length) !== info.size ||
      (await sha256(bytes)) !== info.sha256
    )
      throw Error("ERR_ARTIFACT_HASH");
    return bytes;
  };
  const [wasm, zkey] = await Promise.all([
    read("wasm"),
    read("zkey"),
    read("verification_key"),
  ]);
  return { wasm, zkey };
}
type Request = {
  id: string;
  type: "create" | "import" | "prove" | "clear";
  identity?: string;
  path?: EligibilityPath;
  expected?: Snapshot;
  context?: MembershipContext;
  ballot?: Uint8Array;
};
const worker = self as unknown as DedicatedWorkerGlobalScope;
// Serialize requests: secret state never races an import/reset while proving.
let queue = Promise.resolve();
worker.onmessage = (event: MessageEvent<Request>) => {
  queue = queue.then(async () => {
    const req = event.data;
    try {
      let result: unknown;
      if (req.type === "create") {
        identity = new Identity();
        result = { commitment: identity.commitment.toString() };
      } else if (req.type === "import") {
        if (typeof req.identity !== "string") throw Error("ERR_IDENTITY");
        identity = Identity.import(req.identity);
        result = { commitment: identity.commitment.toString() };
      } else if (req.type === "clear") {
        identity = undefined;
        artifacts.clear();
        result = { cleared: true };
      } else if (req.type === "prove") {
        if (
          !identity ||
          !req.path ||
          !req.expected ||
          !req.context ||
          !(req.ballot instanceof Uint8Array)
        )
          throw Error("ERR_REQUEST");
        checkContext(req.context, req.expected);
        await verifyPath(
          req.path,
          req.expected,
          identity.commitment.toString(),
        );
        const { scope, message } = await binding(
          req.context.network_id,
          req.context.poll_id,
          req.ballot,
        );
        const depth = req.expected.circuit_depth;
        if (!artifacts.has(depth))
          artifacts.set(
            depth,
            load(depth).catch((error) => {
              artifacts.delete(depth);
              throw error;
            }),
          );
        const verified = await artifacts.get(depth)!;
        // The SDK pads siblings in-place. Always give it a private copy of the
        // compressed LeanIMT path; the original path remains immutable.
        const path = {
          root: BigInt(req.path.snapshot.root!),
          leaf: BigInt(req.path.leaf),
          index: Number(req.path.index),
          siblings: req.path.siblings.map(BigInt),
        };
        const proof = await generateProof(
          identity,
          path,
          message,
          scope,
          Number(depth),
          verified,
        );
        const membership: Membership = {
          merkleTreeDepth: depth,
          merkleTreeRoot: proof.merkleTreeRoot,
          nullifier: proof.nullifier,
          message: proof.message,
          scope: proof.scope,
          points: [...proof.points],
        };
        if (!(await verifyMembership(req.context, req.ballot, membership)))
          throw Error("ERR_GENERATED_PROOF");
        result = { membership };
      } else throw Error("ERR_REQUEST");
      worker.postMessage({ id: req.id, result });
    } catch (error) {
      worker.postMessage({
        id: req.id,
        error: error instanceof Error ? error.message : "ERR_PROOF",
      });
    }
  });
};
