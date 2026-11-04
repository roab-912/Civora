import {
  uint,
  type Config,
  type State,
  type Event,
  type Transaction,
} from "./protocol.js";
export interface Policy {
  collection_ms: string;
  queue_ms: string;
  setup_ms: string;
  notice_ms: string;
  voting_ms: string;
  tally_ms: string;
  endorsements_required: string;
  ordinary_quorum_bps: string;
  constitutional_quorum_bps: string;
  ordinary_execution_delay_ms: string;
  constitutional_execution_delay_ms: string;
}
export interface Guardian {
  account_id: string;
  signing_pk: string;
}
export interface GovernanceConfig {
  policy: Policy;
  guardians: Guardian[];
  crypto_profile: "unconfigured" | "simulation_v0_2";
  eligibility_suite: string;
  ballot_suite: string;
  tally_suite: string;
  effect_profile: "ledger_only_v1";
}
export interface LawContent {
  title: string;
  text: string;
  language: string;
}
export interface RoleContent {
  label: string;
  description: string;
  capabilities: (
    | "annotate_proposal"
    | "publish_notice"
    | "report_implementation"
  )[];
  scope_id: string | null;
}
interface Changes {
  "law.create": LawContent;
  "law.amend": LawContent;
  "law.repeal": Record<string, never>;
  "law.restore": { source_revision: string };
  "role.create": RoleContent;
  "role.amend": RoleContent;
  "role.retire": Record<string, never>;
  "role.restore": { source_revision: string };
  "role.grant": { account_id: string; until_ms: string };
  "role.revoke": { grant_id: string };
  "policy.update": { policy: Policy };
}
export type Submission = {
  [A in keyof Changes]: {
    action: A;
    target_id: A extends "law.create" | "role.create" | "policy.update"
      ? null
      : string;
    expected_revision: A extends "law.create" | "role.create" ? null : string;
    rationale: string;
    requested_effect_ms: string | null;
    changes: Changes[A];
  };
}[keyof Changes];
type CommonBody = { network_id: string } & (
  | { v: "1"; protocol_epoch?: never }
  | { v: "2"; protocol_epoch: string }
);
export type SignedGovernanceBody = CommonBody & {
  actor_id: string;
  nonce: string;
} & (
    | { type: "proposal.submit"; payload: Submission }
    | { type: "proposal.endorse"; payload: { proposal_id: string } }
    | {
        type: "role.publish";
        payload: {
          role_id: string;
          capability: RoleContent["capabilities"][number];
          target_id: string;
          content: string;
        };
      }
  );
export interface Membership {
  merkleTreeDepth: "16";
  merkleTreeRoot: string;
  nullifier: string;
  message: string;
  scope: string;
  points: string[];
}
export type ProofBody = CommonBody & { actor_id: null; nonce: null } & (
    | {
        type: "poll.configure";
        payload: {
          poll_id: string;
          ceremony_bundle: string;
          guardian_acks: { account_id: string; signature: string }[];
        };
      }
    | {
        type: "vote.cast";
        payload: {
          poll_id: string;
          encrypted_ballot: string;
          membership: Membership;
        };
      }
    | {
        type: "poll.tally";
        payload: {
          poll_id: string;
          yes: string;
          no: string;
          aggregate_digest: string;
          decryption_record: string;
        };
      }
  );
export type GovernanceTransaction =
  | {
      body: SignedGovernanceBody;
      auth: { scheme: "ed25519"; signature: string };
    }
  | { body: ProofBody; auth: { scheme: "proof" } };
export interface GovernanceFixture {
  name: string;
  genesis: Config & { governance: GovernanceConfig };
  blocks: {
    height: string;
    time_ms: string;
    transactions: (GovernanceTransaction | Transaction)[];
  }[];
  expected_roots: string[];
  expected_events: Event[][];
  expected_state: State;
}
const hour = 3600000n,
  day = 24n * hour;
export function validatePolicy(p: Policy): void {
  for (const s of [
    p.collection_ms,
    p.queue_ms,
    p.setup_ms,
    p.notice_ms,
    p.voting_ms,
    p.tally_ms,
    p.ordinary_execution_delay_ms,
    p.constitutional_execution_delay_ms,
  ]) {
    const n = uint(s);
    if (n < hour || n > 30n * day) throw Error("ERR_POLICY_DURATION");
  }
  if (
    uint(p.voting_ms) > 14n * day ||
    uint(p.constitutional_execution_delay_ms) < day
  )
    throw Error("ERR_POLICY_DURATION");
  if (uint(p.endorsements_required) < 1n || uint(p.endorsements_required) > 20n)
    throw Error("ERR_POLICY_ENDORSEMENTS");
  if (
    uint(p.ordinary_quorum_bps) > 5000n ||
    uint(p.constitutional_quorum_bps) < 2000n ||
    uint(p.constitutional_quorum_bps) > 5000n
  )
    throw Error("ERR_POLICY_QUORUM");
}
export function decisionThreshold(
  yes: string,
  no: string,
  eligible: string,
  p: Policy,
  constitutional: boolean,
): boolean {
  const y = uint(yes),
    n = uint(no),
    e = uint(eligible),
    c = y + n;
  if (c > 0xffffffffffffffffn || c > e) throw Error("ERR_TALLY_COUNT");
  if (c === 0n) return false;
  const majority = constitutional ? 3n * y >= 2n * c : 2n * y > c;
  const quorum = uint(
    constitutional ? p.constitutional_quorum_bps : p.ordinary_quorum_bps,
  );
  return majority && 10000n * c >= quorum * e;
}
