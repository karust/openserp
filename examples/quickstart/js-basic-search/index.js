import { OpenSERP } from "@openserp/sdk";

// Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
//   const client = new OpenSERP({ apiKey: "<YOUR_API_TOKEN>" });
const client = new OpenSERP({ baseUrl: "http://localhost:7000" });

const { results } = await client.search({
  engine: "google",
  text: "open source search api",
  region: "US",
  lang: "EN",
  limit: 10,
});

for (const item of results) {
  console.log(`${item.rank}. ${item.title}`);
  console.log(`   ${item.url}`);
}
