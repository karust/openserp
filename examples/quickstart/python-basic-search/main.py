from openserp import OpenSERP

with OpenSERP(base_url="http://localhost:7000") as client:
    response = client.search(
        engine="google",
        text="open source search api",
        region="US",
        lang="EN",
        limit=10,
    )

    for item in response.results:
        print(f"{item.rank}. {item.title}")
        print(f"   {item.url}")

    if client.last_response:
        print("Request ID:", client.last_response.request_id)
