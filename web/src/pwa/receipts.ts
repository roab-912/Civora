import { canonical, uint, validate } from "./protocol.js";
import type { Row } from "./network.js";

export function receiptDocumentType(value: Row): "ballot" | "result" {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("ERR_RECEIPT_FORMAT");
  if (value.format !== undefined) {
    if (value.format === "civora-result-report-1") return "result";
    throw Error("ERR_RECEIPT_FORMAT");
  }
  if (value.ballot && !("yes" in value) && !("no" in value)) return "ballot";
  if ("yes" in value && "no" in value && "as_of_height" in value)
    return "result";
  if (value.proof?.path) throw Error("ERR_RECEIPT_STATE_PROOF");
  if (value.header && value.ciphertext) throw Error("ERR_RECEIPT_BACKUP");
  throw Error("ERR_RECEIPT_FORMAT");
}

export function inspectResultReport(
  value: unknown,
  network: string,
  genesis: string,
): Row {
  let report: Row;
  try {
    report = validate<Row>("pwa_result_report", value);
  } catch {
    throw Error("ERR_RESULT_REPORT_FORMAT");
  }
  if (
    report.network_id !== network ||
    (report.genesis_sha256 && report.genesis_sha256 !== genesis)
  )
    throw Error("ERR_NETWORK");
  if (uint(report.as_of_height) === 0n) throw Error("ERR_RESULT_REPORT_FORMAT");
  const total = uint(report.yes) + uint(report.no);
  if (
    total > 18446744073709551615n ||
    (report.counted !== undefined &&
      (uint(report.counted) !== total || uint(report.admitted) < total))
  )
    throw Error("ERR_RESULT_REPORT_MISMATCH");
  return report;
}

// Uploaded summaries and their `verified` flag are never an authority. Compare
// only after the caller has independently checked the actual network proofs.
export function compareResultReport(uploaded: Row, checked: Row): void {
  if (uploaded.as_of_height !== checked.as_of_height)
    throw Error("ERR_RESULT_REPORT_CHANGED");
  const summary = (value: Row) =>
    Object.fromEntries(
      Object.entries(value).filter(
        ([key]) => !["format", "genesis_sha256"].includes(key),
      ),
    );
  if (canonical(summary(uploaded)) !== canonical(summary(checked)))
    throw Error("ERR_RESULT_REPORT_MISMATCH");
}
