import csv

from openserp import OpenSERP

target_domain = "go.dev"
keywords = ["golang tutorial", "go programming language", "golang documentation"]
output_file = "rankings.csv"


def matches(domain: str | None, target: str) -> bool:
    if not domain:
        return False
    domain = domain.lower().removeprefix("www.")
    return domain == target or domain.endswith(f".{target}")


# Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
#   with OpenSERP(api_key="<YOUR_API_TOKEN>") as client:
with OpenSERP(base_url="http://localhost:7000") as client:
    rows = []
    for keyword in keywords:
        response = client.search(engine="google", text=keyword, region="US", limit=20)
        hit = next((r for r in response.results if matches(r.domain, target_domain)), None)
        rows.append(
            {
                "keyword": keyword,
                "rank": hit.rank if hit else "not in top 20",
                "url": hit.url if hit else "",
            }
        )
        print(f'"{keyword}": {rows[-1]["rank"]}')

with open(output_file, "w", newline="", encoding="utf-8") as handle:
    writer = csv.DictWriter(handle, fieldnames=["keyword", "rank", "url"])
    writer.writeheader()
    writer.writerows(rows)

print(f"\nSaved rankings for {target_domain} to {output_file}")
