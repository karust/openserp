# Agent search tool (Python)

A `web_search(query)` function shaped to register as a tool for an LLM agent.
It returns compact JSON-serializable results the model can read and cite.

```bash
pip install -r requirements.txt
python main.py
```

Import `web_search` from [main.py](main.py) and register it with your agent framework.
