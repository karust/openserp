from openserp import OpenSERP

url = "https://go.dev/doc/"
output_file = "extracted.md"

# Hosted API instead? Get a key at https://openserp.org/dashboard/keys:
#   with OpenSERP(api_key="<YOUR_API_TOKEN>", timeout=60.0) as client:
with OpenSERP(base_url="http://localhost:7000", timeout=60.0) as client:
    # /extract turns a single page into clean Markdown (or plain text).
    result = client.extract(url=url, mode="auto", clean=True)

content = result.markdown or result.text or ""
with open(output_file, "w", encoding="utf-8") as handle:
    handle.write(content)

print(f"Extracted {len(content)} characters from {url} into {output_file}")
