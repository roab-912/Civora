export interface Subscription {
  v: "1";
  subscription_id: string;
  unsubscribe_secret: string;
  endpoint: string;
  keys: { p256dh: string; auth: string };
  networks: string[];
  enabled: boolean;
}
export type StoredSubscription = Omit<Subscription, "unsubscribe_secret"> & {
  secret_hash: string;
};
export interface Message {
  v: "1";
  type: "poll.opened";
  event_id: string;
  network_id: string;
  poll_id: string;
  closes_at_ms: string;
}
export interface News extends Message {
  proposal_id: string;
  opened_at_ms: string;
  source_height: string;
}
export interface Delivery {
  status: number;
  retryAfter?: number;
  expired?: boolean;
}
export interface Sender {
  send(
    subscription: StoredSubscription,
    message: Message,
    now: number,
  ): Promise<Delivery>;
}
