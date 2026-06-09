from openserp import OpenSERP

# Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
#   with OpenSERP(api_key="<YOUR_API_TOKEN>") as client:
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
