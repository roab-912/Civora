import { spawnSync } from "node:child_process";
import { resolve } from "node:path";
const result = spawnSync(
  process.execPath,
  ["node_modules/playwright/cli.js", "install", "chromium", "webkit"],
  {
    env: {
      ...process.env,
      PLAYWRIGHT_BROWSERS_PATH: resolve(".tools/playwright"),
    },
    stdio: "inherit",
  },
);
if (result.status !== 0) process.exit(result.status ?? 1);
