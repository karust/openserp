import csv

from openserp import OpenSERP


keywords = ["search api", "open source serp"]
output_file = "keywords.csv"

# Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
#with OpenSERP(api_key="<YOUR_API_TOKEN>") as client:
with OpenSERP(base_url="http://localhost:7000") as client:
    rows = []
    for keyword in keywords:
        response = client.search(engine="google", text=keyword, region="US", limit=10)
        for result in response.results:
            rows.append(
                {
                    "keyword": keyword,
                    "rank": result.rank,
                    "title": result.title,
                    "url": result.url,
                    "domain": result.domain,
                }
            )

with open(output_file, "w", newline="", encoding="utf-8") as handle:
    writer = csv.DictWriter(handle, fieldnames=["keyword", "rank", "title", "url", "domain"])
    writer.writeheader()
    writer.writerows(rows)

print(f"Wrote {len(rows)} rows to {output_file}")
