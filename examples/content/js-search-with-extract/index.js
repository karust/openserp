import { OpenSERP } from "@openserp/sdk";

// Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
//   const client = new OpenSERP({ apiKey: "<YOUR_API_TOKEN>", timeoutMs: 60_000 });
const client = new OpenSERP({ baseUrl: "http://localhost:7000", timeoutMs: 60_000 });

// `extract: N` fetches the top N pages (max 5) and returns their cleaned
// content alongside each result, so you get the page text in a single request.
// `extract: true` is shorthand for the top result.
const { results } = await client.search({
  engine: "ecosia",
  text: "what is a serp api",
  extract: 2,
  extractMode: "auto",
});

for (const item of results) {
  console.log(`${item.rank}. ${item.title}`);
  console.log(`   ${item.url}`);

  // When extracted, `item.extracted.content` holds the page body. Trim it to
  // a short preview here.
  const content = item.extracted?.content;
  if (content) {
    console.log(`   ${content.slice(0, 300).trim()}…`);
  }
  console.log();
}
