import { readFileSync } from "node:fs";
import { resolve } from "node:path";
process.env.PLAYWRIGHT_BROWSERS_PATH ??= resolve(".tools/playwright");
const { chromium } = await import("@playwright/test"),
  browser = await chromium.launch({ headless: true });
try {
  for (const size of [192, 512]) {
    const page = await browser.newPage({
      viewport: { width: size, height: size },
      deviceScaleFactor: 1,
    });
    await page.setContent(
      '<body style="margin:0">' +
        readFileSync("web/pwa/icon.svg", "utf8").replace(
          'viewBox="0 0 96 96"',
          `width="${size}" height="${size}" viewBox="0 0 96 96"`,
        ) +
        "</body>",
    );
    await page.screenshot({
      path: `web/pwa/icon-${size}.png`,
      omitBackground: true,
    });
    await page.close();
  }
} finally {
  await browser.close();
}
