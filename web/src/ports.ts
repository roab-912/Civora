export interface SnapshotRef {
  networkID: string;
  height: string;
  root: string;
  suiteID: string;
  epoch: string;
}
export interface EligibilitySnapshot {
  freeze(previousHeight: string): SnapshotRef;
  path(snapshot: SnapshotRef, commitment: string): Uint8Array;
}
export interface BulletinContext {
  networkID: string;
  pollID: string;
  epoch: string;
  eligibilitySuite: string;
  ballotSuite: string;
  tallySuite: string;
}
export interface BulletinTransport {
  submit(
    context: BulletinContext,
    envelope: Uint8Array,
  ): Promise<{ envelopeDigest: string; proof: Uint8Array }>;
}
export interface ResultProof {
  verify(
    context: BulletinContext,
    aggregate: Uint8Array,
    proof: Uint8Array,
  ): void;
}

export interface MembershipVerifier {
  verifyMembership(
    context: BulletinContext,
    root: string,
    depth: "16" | "26",
    ballot: Uint8Array,
    proof: Uint8Array,
  ): void;
}
export interface ProtocolVersion {
  beforeBlock(height: string): void;
  authorize(
    epoch: string,
    operation: string,
    moduleID: string,
    historical: boolean,
  ): void;
}
export interface LedgerStore {
  height(): string;
  appHash(): string;
  commit(block: Uint8Array): void;
}

// Pure governance ports; the transition engine performs no network I/O.
export interface EligibilityProvider {
  snapshot(request: {
    networkID: string;
    epoch: string;
    pollID: string;
    height: string;
    accounts: { id: string; commitment: string }[];
  }): { root: string | null; commitments: string[] };
}
export interface PollContext extends BulletinContext {
  proposalID: string;
  groupRoot: string | null;
  commitments: string[];
  ceremonyDigest: string;
  ceremonyBundle: string;
  guardianIDs: string[];
}
export interface VotingCrypto {
  configure(context: PollContext, bundle: Uint8Array): string;
  accept(
    context: PollContext,
    aggregate: string,
    payload: Uint8Array,
  ): {
    nullifier: string;
    ballotID: string;
    ciphertextDigest: string;
    confirmationCode: string;
    aggregate: string;
  };
  tally(
    context: PollContext,
    aggregate: string,
    count: string,
    payload: Uint8Array,
  ): { yes: string; no: string };
}
export interface EffectAdapter {
  evaluate(request: {
    networkID: string;
    proposalID: string;
    action: string;
    effectiveMS: string;
    payload: Uint8Array;
  }): { profile: "ledger_only_v1"; reference: string };
}
