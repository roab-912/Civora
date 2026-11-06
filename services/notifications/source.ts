import {
  canonical,
  parse,
  sha,
  domain,
  uint,
  validate,
  chainID,
} from "../../web/src/protocol.js";
import {
  verifyPublicState,
  type PublicProof,
} from "../../web/src/citizen-audit.js";
import { verifyHeader, type Trust } from "../../web/src/consensus.js";
import type { News } from "./types.js";
import type { NotificationStore } from "./store.js";
type Row = Record<string, any>;
export interface Watch {
  name: string;
  manifest: Row;
  api_roots: string[];
}
function trust(g: Row, h: string): Trust {
  let keys = g.validators.map((v: Row) => {
    if (v.power !== "1" || v.pub_key.type !== "tendermint/PubKeyEd25519")
      throw Error("ERR_VALIDATORS");
    return Buffer.from(v.pub_key.value, "base64").toString("hex");
  });
  if (keys.length !== 7 || new Set(keys).size !== 7)
    throw Error("ERR_VALIDATORS");
  for (const r of g.app_state.consensus_rotations ?? [])
    if (uint(h) >= uint(r.height) + 2n) keys = r.keys;
  return { chain_id: g.chain_id, keys };
}
export function verifySourceProof(
  genesis: Row,
  p: PublicProof,
  table: string,
  key: unknown,
  height: string,
): void {
  validate("citizen_state_proof", p);
  verifyPublicState(trust(genesis, String(uint(height) + 1n)), p);
  if (
    p.height !== height ||
    p.path.entry.table !== table ||
    canonical(p.path.entry.key) !== canonical(key) ||
    !p.path.exists
  )
    throw Error("ERR_PROOF_CONTEXT");
}
export class FinalizedNewsSource {
  private genesis: Row | null = null;
  constructor(readonly watch: Watch) {
    validate("citizen_manifest", watch.manifest);
  }
  private async get(
    path: string,
    accept: (value: any) => void = () => {},
  ): Promise<any> {
    let last: unknown;
    for (const origin of this.watch.api_roots)
      try {
        const r = await fetch(origin + path, {
          redirect: "error",
          signal: AbortSignal.timeout(10000),
        });
        if (!r.ok) throw Error("ERR_SOURCE_UNAVAILABLE");
        if (!r.body) throw Error("ERR_SOURCE_UNAVAILABLE");
        const chunks: Uint8Array[] = [],
          reader = r.body.getReader();
        let size = 0;
        try {
          for (;;) {
            const item = await reader.read();
            if (item.done) break;
            size += item.value.length;
            if (size > 8 * 1024 * 1024) {
              await reader.cancel();
              throw Error("ERR_CAPACITY");
            }
            chunks.push(item.value);
          }
        } finally {
          reader.releaseLock();
        }
        const raw = Buffer.concat(chunks);
        const value = path === "/v1/genesis" ? raw : parse(raw);
        accept(value);
        return value;
      } catch (e) {
        last = e;
      }
    throw last;
  }
  private async proof(
    table: string,
    key: unknown,
    height: string,
  ): Promise<Row> {
    return this.get(
      `/v1/proofs/state?height=${height}&table=${table}&key=${Buffer.from(canonical(key)).toString("base64url")}`,
      (value) => {
        const p = value as PublicProof;
        verifySourceProof(this.genesis!, p, table, key, height);
      },
    );
  }
  async sync(store: NotificationStore, now: number): Promise<number> {
    const manifest = this.watch.manifest,
      network = manifest.network_id as string;
    if (!this.genesis) {
      const raw = (await this.get("/v1/genesis", (b: Buffer) => {
        if (
          sha(b).toString("hex") !== manifest.main.genesis.sha256 ||
          String(b.length) !== manifest.main.genesis.size
        )
          throw Error("ERR_GENESIS");
      })) as Buffer;
      const g = JSON.parse(raw.toString());
      if (
        g.chain_id !== manifest.main.chain_id ||
        g.chain_id !== chainID(network)
      )
        throw Error("ERR_NETWORK");
      this.genesis = g;
    }
    const status = await this.get("/v1/network");
    if (status.network_id !== network) throw Error("ERR_NETWORK");
    const maximum = uint(status.height) - 1n;
    if (maximum < 1n) throw Error("ERR_PROOF_PENDING");
    const clockProof = await this.proof("network", "config", String(maximum)),
      clock = clockProof.path.entry.value as Row;
    if (clock.config.network_id !== network) throw Error("ERR_NETWORK");
    const header = verifyHeader(
        trust(this.genesis!, String(maximum + 1n)),
        clockProof.signed_header,
      ),
      headerTime = Date.parse(header.time);
    if (!Number.isFinite(headerTime) || headerTime < 0)
      throw Error("ERR_SOURCE_TIME");
    store.observeClock(
      network,
      String(maximum),
      String(
        BigInt(clock.time_ms) > BigInt(headerTime)
          ? BigInt(clock.time_ms)
          : BigInt(headerTime),
      ),
    );
    let after = store.cursor(network),
      total = 0;
    for (let pages = 0; pages < 4; pages++) {
      const page = await this.get(
          "/v1/events?limit=100" +
            (after ? "&after=" + encodeURIComponent(after) : ""),
        ),
        news: News[] = [];
      if (!Array.isArray(page.items) || page.items.length > 100)
        throw Error("ERR_SOURCE_EVENTS");
      let next = after,
        final = true;
      for (const event of page.items as Row[]) {
        const h = uint(event.height),
          position = uint(event.position);
        if (h > maximum) {
          final = false;
          break;
        }
        const cursor = event.height + ":" + event.position;
        if (next) {
          const [a, b] = next.split(":").map(BigInt);
          if (h < a! || (h === a && position <= b!))
            throw Error("ERR_SOURCE_CURSOR");
        }
        if (event.type === "proposal.open") {
          const proposalID = event.attributes?.subject;
          if (!/^[0-9a-f]{64}$/.test(proposalID))
            throw Error("ERR_SOURCE_EVENTS");
          const p = (await this.proof("proposals", proposalID, event.height))
              .path.entry.value as Row,
            pollID = p.poll_id;
          if (p.state !== "open" || !/^[0-9a-f]{64}$/.test(pollID))
            throw Error("ERR_EVENT_CONTEXT");
          const cfg = (await this.proof("network", "config", event.height)).path
            .entry.value as Row;
          let opens: string, closes: string;
          if (manifest.voting_profile === "transport_dev_v0_6") {
            const f = (
              await this.proof("transport_polls", pollID, event.height)
            ).path.entry.value as Row;
            if (
              f.context.network_id !== network ||
              f.context.poll_id !== pollID ||
              f.proposal_id !== proposalID ||
              f.source_height !== event.height
            )
              throw Error("ERR_EVENT_CONTEXT");
            opens = f.opens_at_ms;
            closes = f.closes_at_ms;
          } else {
            const poll = (await this.proof("polls", pollID, event.height)).path
              .entry.value as Row;
            if (
              poll.state !== "open" ||
              poll.proposal_id !== proposalID ||
              uint(poll.snapshot_height) + 1n !== h
            )
              throw Error("ERR_EVENT_CONTEXT");
            opens = poll.opens_at_ms;
            closes = poll.closes_at_ms;
          }
          if (
            cfg.config.network_id !== network ||
            uint(cfg.time_ms) < uint(opens) ||
            uint(cfg.time_ms) >= uint(closes)
          )
            throw Error("ERR_EVENT_CONTEXT");
          news.push({
            v: "1",
            type: "poll.opened",
            network_id: network,
            poll_id: pollID,
            proposal_id: proposalID,
            event_id: domain("push/poll.opened", [network, pollID]),
            opened_at_ms: opens,
            closes_at_ms: closes,
            source_height: event.height,
          });
        }
        next = cursor;
      }
      store.project(network, next, news, now);
      total += news.length;
      if (!final || next === after || page.items.length < 100) break;
      after = next;
    }
    return total;
  }
}
