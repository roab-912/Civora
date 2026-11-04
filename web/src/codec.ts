// Shared strict protocol codec, usable in browsers and workers.
export type JSONValue =
  | null
  | boolean
  | string
  | JSONValue[]
  | { [key: string]: JSONValue };
export function canonical(value: unknown): string {
  if (value === null || typeof value === "boolean")
    return JSON.stringify(value);
  if (typeof value === "string") {
    if (
      !value.isWellFormed() ||
      value.normalize("NFC") !== value ||
      value.includes("\r")
    )
      throw Error("text must be Unicode NFC with LF endings");
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) return "[" + value.map(canonical).join(",") + "]";
  if (
    typeof value === "object" &&
    value &&
    Object.getPrototypeOf(value) === Object.prototype
  ) {
    return (
      "{" +
      Object.keys(value)
        .sort()
        .map(
          (k) =>
            canonical(k) +
            ":" +
            canonical((value as Record<string, unknown>)[k]),
        )
        .join(",") +
      "}"
    );
  }
  throw Error("unsupported JSON type (integers must be strings)");
}
export function parse(raw: Uint8Array | string): unknown {
  const text =
    typeof raw === "string"
      ? raw
      : new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(raw);
  const value: unknown = JSON.parse(text);
  if (canonical(value) !== text) throw Error("noncanonical JSON");
  return value;
}
