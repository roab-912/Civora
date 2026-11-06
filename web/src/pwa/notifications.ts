import { read, write, remove } from "./storage.js";
import { randomHex, canonical, parse, validate } from "./protocol.js";
export interface NotificationSettings {
  api_root: string;
  subscription_id: string;
  unsubscribe_secret: string;
  networks: string[];
  enabled: boolean;
  pending_delete: boolean;
}
export async function settings() {
  return read<NotificationSettings>("settings", "notifications");
}
export function iosNeedsInstallation(): boolean {
  const ios =
    /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
  return (
    ios &&
    !matchMedia("(display-mode: standalone)").matches &&
    !(navigator as Navigator & { standalone?: boolean }).standalone
  );
}
export function pushCapability(): string | null {
  if (!isSecureContext || !crypto.subtle)
    return "Ouvrez cette page en HTTPS pour activer les notifications.";
  if (iosNeedsInstallation())
    return "Sur iPhone ou iPad, ouvrez Partager → Sur l’écran d’accueil, puis lancez l’application installée. Les actualités restent consultables ici.";
  if (
    !("Notification" in window) ||
    !("PushManager" in window) ||
    !("serviceWorker" in navigator)
  )
    return "Ce navigateur ne propose pas les Push. La boîte d’actualités reste disponible.";
  if (Notification.permission === "denied")
    return "Les notifications sont refusées dans les réglages du navigateur. Vous pouvez continuer avec la boîte d’actualités.";
  return null;
}
async function api(root: string, path: string, input?: unknown) {
  if (!/^\/notifications$/.test(root)) throw Error("ERR_NOTIFICATION_SERVICE");
  const r = await fetch(root + path, {
    signal: AbortSignal.timeout(15000),
    method: input ? "POST" : "GET",
    credentials: "omit",
    cache: "no-store",
    redirect: "error",
    headers: { "Content-Type": "application/json" },
    ...(input ? { body: canonical(input) } : {}),
  });
  const value = parse(new TextEncoder().encode(await r.text())) as Record<
    string,
    any
  >;
  if (!r.ok) throw Error(String(value.code ?? "ERR_NOTIFICATION_SERVICE"));
  return value;
}
// Called directly by a user click: permission is requested before the first
// asynchronous operation, including on Safari where the gesture is required.
export async function enablePush(
  root: string,
  networks: string[],
): Promise<void> {
  const reason = pushCapability();
  if (reason) throw Error(reason);
  const permission = await Notification.requestPermission();
  if (permission !== "granted")
    throw Error(
      "Les notifications ne sont pas autorisées. Les actualités restent accessibles et vous pouvez voter normalement.",
    );
  const registration = await navigator.serviceWorker.ready;
  if (registration.waiting || registration.installing)
    throw Error(
      "Fermez les autres onglets Civora puis rouvrez l’application pour terminer sa mise à jour avant d’activer les notifications.",
    );
  const config = await api(root, "/v1/config");
  if (
    !config.enabled ||
    config.v !== "1" ||
    !Array.isArray(config.networks) ||
    networks.some((n) => !config.networks.includes(n))
  )
    throw Error("ERR_NOTIFICATION_SERVICE");
  const decoded = Uint8Array.from(
    atob(
      String(config.vapid_public_key).replaceAll("-", "+").replaceAll("_", "/"),
    ),
    (c) => c.charCodeAt(0),
  );
  let subscription = await registration.pushManager.getSubscription();
  if (
    subscription &&
    JSON.stringify([
      ...new Uint8Array(
        subscription.options.applicationServerKey ?? new ArrayBuffer(0),
      ),
    ]) !== JSON.stringify([...decoded])
  ) {
    await subscription.unsubscribe();
    subscription = null;
  }
  subscription ??= await registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: decoded,
  });
  const old = await settings(),
    state: NotificationSettings = {
      api_root: root,
      subscription_id: old?.subscription_id ?? randomHex(32),
      unsubscribe_secret: old?.unsubscribe_secret ?? randomHex(32),
      networks: [...new Set(networks)].sort(),
      enabled: true,
      pending_delete: false,
    },
    json = subscription.toJSON();
  const input = {
    v: "1",
    subscription_id: state.subscription_id,
    unsubscribe_secret: state.unsubscribe_secret,
    endpoint: subscription.endpoint,
    keys: json.keys,
    networks: state.networks,
    enabled: true,
  };
  validate("push_subscription", input);
  // Save the capability before POST so a lost response cannot orphan the endpoint.
  await write("settings", "notifications", { ...state, enabled: false });
  await api(root, "/v1/subscriptions", input);
  await write("settings", "notifications", state);
}
export async function updatePreferences(networks: string[]): Promise<void> {
  const state = await settings();
  if (!state?.enabled) throw Error("Les Push ne sont pas activés.");
  if (!networks.length) throw Error("Choisissez au moins un réseau.");
  const registration = await navigator.serviceWorker.ready,
    s = await registration.pushManager.getSubscription();
  if (!s) throw Error("ERR_NOTIFICATION_SERVICE");
  const next = { ...state, networks: [...new Set(networks)].sort() },
    input = {
      v: "1",
      subscription_id: next.subscription_id,
      unsubscribe_secret: next.unsubscribe_secret,
      endpoint: s.endpoint,
      keys: s.toJSON().keys,
      networks: next.networks,
      enabled: true,
    };
  validate("push_subscription", input);
  await api(next.api_root, "/v1/subscriptions", input);
  await write("settings", "notifications", next);
}
export async function disablePush(): Promise<boolean> {
  const state = await settings();
  if (!state) return true;
  await write("settings", "notifications", {
    ...state,
    enabled: false,
    pending_delete: true,
  });
  let nativeRemoved = true;
  try {
    if ("serviceWorker" in navigator) {
      const registration = await navigator.serviceWorker.getRegistration();
      await (await registration?.pushManager.getSubscription())?.unsubscribe();
    }
  } catch {
    nativeRemoved = false;
  }
  try {
    await api(state.api_root, "/v1/unsubscribe", {
      v: "1",
      subscription_id: state.subscription_id,
      unsubscribe_secret: state.unsubscribe_secret,
    });
    if (nativeRemoved) await remove("settings", "notifications");
    return nativeRemoved;
  } catch {
    return false;
  }
}
export async function getNews(root: string, network: string, cursor = "") {
  const value = await api(
    root,
    "/v1/news?network_id=" +
      network +
      (cursor ? "&cursor=" + encodeURIComponent(cursor) : ""),
  );
  if (
    value.network_id !== network ||
    !Array.isArray(value.items) ||
    value.items.length > 100 ||
    typeof value.next_cursor !== "string"
  )
    throw Error("ERR_NETWORK");
  for (const item of value.items) {
    validate("news_item", item);
    if (item.network_id !== network) throw Error("ERR_NETWORK");
  }
  return value as {
    items: Record<string, string>[];
    next_cursor: string;
    source_available: boolean;
  };
}
