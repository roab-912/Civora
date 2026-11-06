export function notificationSchemas(
  d,
  { object, list, ref, id, uint, constant },
) {
  const text = (n) => ({ type: "string", minLength: 1, maxLength: n }),
    b64 = (n) => ({ ...text(n), pattern: "^[A-Za-z0-9_-]+$" });
  d.push_keys = object({
    p256dh: { ...b64(87), minLength: 87 },
    auth: { ...b64(22), minLength: 22 },
  });
  d.push_subscription = object({
    v: constant("1"),
    subscription_id: id,
    unsubscribe_secret: id,
    endpoint: text(4096),
    keys: ref("push_keys"),
    networks: { ...list(id, 16), minItems: 1, uniqueItems: true },
    enabled: { type: "boolean" },
  });
  d.push_unsubscribe = object({
    v: constant("1"),
    subscription_id: id,
    unsubscribe_secret: id,
  });
  d.push_message = object({
    v: constant("1"),
    type: constant("poll.opened"),
    event_id: id,
    network_id: id,
    poll_id: id,
    closes_at_ms: uint,
  });
  d.news_item = object({
    ...d.push_message.properties,
    proposal_id: id,
    opened_at_ms: uint,
    source_height: uint,
  });
}
