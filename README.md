# Open SERP (Search Engine Page Results)
Get Google, Yandex, Baidu search engine page results via API or CLI.

## Docker usage
* Run:
```bash
# Use prebuilt image
docker run -p 127.0.0.1:7000:7000 -it karust/openserp serve -a 0.0.0.0 -p 7000

# Or use docker-compose.yaml instead
docker-compose up --build
```

* Get *20* `Google` results for **hello world**, only in *English* - `[serv_addr]/google/search?lang=EN&limit=20&text=hello world`:
```JSON
[
    {
        "rank": 0,
        "url": "https://en.wikipedia.org/wiki/%22Hello,_World!%22_program",
        "title": "\"Hello, World!\" program",
        "description": "A \"Hello, World!\" program is generally a computer program that ignores any input, and outputs or displays a message similar to \"Hello, World!\"."
    },
...
]
```
* You can replace `google` to `yandex` or `baidu` in API call to change search engine.

* Available search parameters:

| Param | Description                                                  |
|-------|--------------------------------------------------------------|
| text  | Text to search                                               |
| lang  | Search pages in selected language (`EN`, `DE`, `RU`...)      |
| date  | Date in `YYYYMMDD..YYYYMMDD` format (e.g. 20181010..20231010) |
| file  | File extension to search  (e.g. `PDF`, `DOC`)                 |
| site  | Search only in selected site                                 |
| limit | Limit the number of results                                  |

## CLI
* Use `-h` flag to see commands.
* You can use `serve` to serve API:
```bash
openserp serve 
```
* Or print results in CLI using `search` command:
```bash
openserp search google "how to get banned in google fast" # You can change `google` to `yandex` or `baidu`
```
As a result you should get JSON output containting search results:
```json
[
 {
  "rank": 0,
  "url": "https://www.cyberoptik.net/blog/6-sure-fire-ways-to-get-banned-from-google/",
  "title": "11 Sure-Fire Ways to Get Banned From Google | CyberOptik",
  "description": "How To Get Banned From Google · 1. Cloaking: The Art of Deception · 2. Plagiarism: Because Originality is Overrated · 3. Keyword Stuffing: More is Always Better · 4 ..."
 },
...
]
 ```