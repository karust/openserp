import json

from openserp import OpenSERP

# A search function shaped for use as an LLM / agent tool: it takes a query
# and returns plain JSON-serializable results the model can read.


def web_search(query: str, limit: int = 10) -> list[dict]:
    """Search the web and return a compact list of results."""
    # Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
    #   with OpenSERP(api_key="<YOUR_API_TOKEN>") as client:
    with OpenSERP(base_url="http://localhost:7000") as client:
        response = client.search(engine="google", text=query, limit=limit)
        return [
            {
                "rank": item.rank,
                "title": item.title,
                "url": item.url,
                "snippet": item.snippet,
            }
            for item in response.results
        ]


if __name__ == "__main__":
    # Register web_search as a tool with your LLM; here we just call it directly.
    results = web_search("what is retrieval augmented generation")
    print(json.dumps(results, indent=2))
