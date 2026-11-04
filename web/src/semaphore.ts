import { verifyProof } from "@semaphore-protocol/proof";
import {
  binding,
  decimal,
  scalarModulus,
  type Membership,
  type MembershipContext,
} from "./eligibility.js";
const coordinateModulus =
  21888242871839275222246405745257275088696311157297823662689037894645226208583n;
// context is authenticated by the caller, including epoch/module authorization.
// Proof bytes alone cannot authenticate a poll manifest or a ledger root.
export async function verifyMembership(
  context: MembershipContext,
  ballot: Uint8Array,
  proof: Membership,
): Promise<boolean> {
  try {
    const expected = await binding(context.network_id, context.poll_id, ballot);
    return await verifyMembershipBound(context, proof, expected);
  } catch {
    return false;
  }
}
// Caller must compute this binding from its authenticated protocol profile.
export async function verifyMembershipBound(
  context: MembershipContext,
  proof: Membership,
  expected: { scope: string; message: string },
): Promise<boolean> {
  try {
    if (
      !["16", "26"].includes(context.circuit_depth) ||
      context.suite_id !== "semaphore-v4.14.5-depth-" + context.circuit_depth ||
      decimal(context.protocol_epoch, 0xffffffffffffffffn) === 0n
    )
      return false;
    if (
      proof.merkleTreeDepth !== context.circuit_depth ||
      proof.merkleTreeRoot !== context.group_root ||
      Object.keys(proof).sort().join(",") !==
        "merkleTreeDepth,merkleTreeRoot,message,nullifier,points,scope"
    )
      return false;
    decimal(proof.merkleTreeRoot, scalarModulus - 1n);
    decimal(proof.nullifier, scalarModulus - 1n);
    if (
      proof.scope !== expected.scope ||
      proof.message !== expected.message ||
      !Array.isArray(proof.points) ||
      proof.points.length !== 8
    )
      return false;
    for (const p of proof.points) decimal(p, coordinateModulus - 1n);
    if (
      (proof.points[0] === "0" && proof.points[1] === "0") ||
      (proof.points[6] === "0" && proof.points[7] === "0") ||
      proof.points.slice(2, 6).every((p) => p === "0")
    )
      return false;
    return await verifyProof({
      ...proof,
      merkleTreeDepth: Number(proof.merkleTreeDepth),
    } as Parameters<typeof verifyProof>[0]);
  } catch {
    return false;
  }
}
