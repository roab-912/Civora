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
