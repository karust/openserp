import { writeFile } from "node:fs/promises";
import { OpenSERP } from "@openserp/sdk";

// Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
//   const client = new OpenSERP({ apiKey: "<YOUR_API_TOKEN>" });
const client = new OpenSERP({ baseUrl: "http://localhost:7000" });

const query = "go gopher mascot";

const { results } = await client.image({
  engine: "bing",
  text: query,
  limit: 12,
});

const cards = results
  .map((result) => {
    const src = escapeHtml(result.image?.thumbnail ?? result.image?.url ?? "");
    const title = escapeHtml(result.title ?? "Untitled");
    const page = escapeHtml(result.source?.page_url ?? "#");
    return `<a class="card" href="${page}"><img src="${src}" alt="${title}"><span>${title}</span></a>`;
  })
  .join("\n");

const html = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${escapeHtml(query)}</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 32px; color: #17202a; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 16px; }
    .card { color: inherit; text-decoration: none; border: 1px solid #d7dde5; border-radius: 8px; overflow: hidden; }
    img { width: 100%; aspect-ratio: 4 / 3; object-fit: cover; background: #f2f4f7; display: block; }
    span { display: block; padding: 10px; font-size: 14px; }
  </style>
</head>
<body>
  <h1>${escapeHtml(query)}</h1>
  <div class="grid">${cards}</div>
</body>
</html>`;

await writeFile("gallery.html", html, "utf8");
console.log(`Wrote ${results.length} images to gallery.html — open it in a browser.`);

function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  })[char]);
}
