import { OpenSERP } from "@openserp/sdk";

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

console.log("Request ID:", client.lastResponse?.requestId);
