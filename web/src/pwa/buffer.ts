import { Buffer } from "buffer";
// buffer 6 supplies the native Comet codec's byte operations; RFC 4648 URL
// encoding is added explicitly rather than silently accepting padded blobs.
if (!Buffer.isEncoding("base64url")) {
  const from = Buffer.from,
    string = Buffer.prototype.toString;
  (Buffer as any).from = (...args: any[]) => {
    if (args[1] === "base64url") {
      args[0] = String(args[0]).replaceAll("-", "+").replaceAll("_", "/");
      args[1] = "base64";
    }
    return Reflect.apply(from, Buffer, args);
  };
  Buffer.prototype.toString = function (
    encoding?: BufferEncoding,
    start?: number,
    end?: number,
  ) {
    if (encoding === "base64url")
      return string
        .call(this, "base64", start, end)
        .replaceAll("+", "-")
        .replaceAll("/", "_")
        .replace(/=+$/, "");
    return string.call(this, encoding, start, end);
  };
}
export { Buffer };
