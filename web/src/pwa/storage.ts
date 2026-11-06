let opened: Promise<IDBDatabase> | undefined;
function database() {
  return (opened ??= new Promise<IDBDatabase>((yes, no) => {
    const req = indexedDB.open("civora-pwa-1", 1);
    req.onupgradeneeded = () => {
      for (const name of ["vault", "public", "settings", "pending"])
        req.result.createObjectStore(name);
    };
    req.onsuccess = () => yes(req.result);
    req.onerror = () => no(Error("ERR_STORAGE"));
  }));
}
export async function read<T>(
  store: string,
  key: string,
): Promise<T | undefined> {
  const db = await database();
  return new Promise((yes, no) => {
    const tx = db.transaction(store),
      req = tx.objectStore(store).get(key);
    req.onsuccess = () => yes(req.result as T | undefined);
    req.onerror = () => no(Error("ERR_STORAGE"));
  });
}
export async function write(
  store: string,
  key: string,
  value: unknown,
): Promise<void> {
  const db = await database();
  return new Promise((yes, no) => {
    const tx = db.transaction(store, "readwrite");
    tx.objectStore(store).put(value, key);
    tx.oncomplete = () => yes();
    tx.onabort = () => no(Error("ERR_STORAGE"));
    tx.onerror = () => no(Error("ERR_STORAGE"));
  });
}
export async function remove(store: string, key: string): Promise<void> {
  const db = await database();
  return new Promise((yes, no) => {
    const tx = db.transaction(store, "readwrite");
    tx.objectStore(store).delete(key);
    tx.oncomplete = () => yes();
    tx.onerror = () => no(Error("ERR_STORAGE"));
  });
}
export async function insertIfAbsent<T>(
  store: string,
  key: string,
  value: T,
): Promise<T> {
  const db = await database();
  return new Promise((yes, no) => {
    const tx = db.transaction(store, "readwrite"),
      bucket = tx.objectStore(store),
      req = bucket.get(key);
    let result: T;
    req.onsuccess = () => {
      result = req.result ?? value;
      if (req.result === undefined) bucket.put(value, key);
    };
    tx.oncomplete = () => yes(result);
    tx.onabort = () => no(Error("ERR_STORAGE"));
    tx.onerror = () => no(Error("ERR_STORAGE"));
  });
}
