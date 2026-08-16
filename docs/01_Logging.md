~/.psl/psl.log
--------------

Every AI request is recorded in `~/.psl/psl.log`, in your home directory — one log for everything psl compiles, wherever you run it from. One request is one JSON object on one line — prompts and responses span many lines, so a line-per-request keeps the file appendable and greppable:

```json
{
  "time": "2026-08-10T06:18:28+09:00",
  "psl_version": "0.0.2",
  "file": "/Users/you/code/psl/examples/fib.go.psl",
  "slot": { "line": 3, "column": 2, "instruction": "fill in the iterative loop" },
  "model": {
    "name": "claude-opus-5",
    "base_url": "https://api.anthropic.com",
    "endpoint": "https://api.anthropic.com/v1/chat/completions", "max_tokens": 2048
  },
  "request": {
    "model": "claude-opus-5", "max_completion_tokens": 2048,
    "messages": [
      { "role": "system", "content": "…" },
      { "role": "user", "content": [
        { "type": "image_url", "image_url": { "url": "data:image/png;base64,…70 bytes elided…" } },
        { "type": "text", "text": "…" }
      ] }
    ]
  },
  "response": { "text": "…", "stop_reason": "end_turn" },
  "usage": { "input_tokens": 412, "output_tokens": 37, "total_tokens": 449 },
  "duration_ms": 3841
}
```

Failed requests are logged too, with `error` in place of `response` — which is what makes the log worth having when a slot comes back wrong.

```shell
jq -r 'select(.error) | [.time, .model.name, .error] | @tsv' ~/.psl/psl.log   # what went wrong
jq -s 'map(.usage.total_tokens // 0) | add' ~/.psl/psl.log                    # tokens spent
```

What each model has spent is asked often enough that psl adds it up itself, so the common case needs no `jq` at all:

```shell
$ psl usage
MODEL          REQUESTS  INPUT  OUTPUT  TOTAL
claude-opus-5        12  14203    1872  16075
gpt-5.6               3   2110     405   2515
TOTAL                15  16313    2277  18590
psl: /Users/you/.psl/psl.log (2026-08-10 to 2026-08-13)
```

It reads the whole log, one row per `model.name`, heaviest first; the table is written to stdout and the log it came from to stderr, so the rows pipe onwards on their own. `input` and `output` are the halves the endpoint reported, and `total` is its own figure — or the two halves added up, for an endpoint that reports them and no sum. A request that failed has no `usage`: it counts in `REQUESTS`, and in an `ERRORS` column that only appears once there is something in it, without moving the tokens. A line too damaged to read — the tail of a log a run was killed in the middle of writing — is counted in a warning on stderr and stepped over.

For anything the table does not answer — a single week, a per-file breakdown, the cost in money — the log is right there:

```shell
jq -s 'map(select(.time > "2026-08-01")) | group_by(.model.name)
       | map({model: .[0].model.name, in: (map(.usage.input_tokens // 0) | add),
              out: (map(.usage.output_tokens // 0) | add)})' ~/.psl/psl.log
```

`request` is the JSON body the endpoint received, verbatim — not psl's idea of it. Every model is spoken to the same way, so it is always an OpenAI chat completions body: the system prompt is the first message, and an attached image rides in the user message as a data URL. Everything psl composed is in there, and nothing else is: no field exists that the API was not sent. It is recorded for failed requests too, which is what makes a rejected call reproducible:

```shell
jq 'select(.error) | .request' ~/.psl/psl.log                                 # what was sent
jq -r '.request.messages[-1].content | if type == "string" then . else .[-1].text end' ~/.psl/psl.log
```

A slot the model searched to resolve carries a `searches` array: every query it asked, the answer it
was given, and the pages that answer rested on. `request` stays the first body, the one that carried
the file — it is the one with the `web_search` tool in it — and `usage` covers every round the slot
took, so a searched slot is not counted as costing only its last request.

```json
"model": { "name": "gpt-5.5", "web_search": "gpt-5-search-api", "…": "…" },
"searches": [
  { "query": "current stable Go release version",
    "answer": "The current stable version of Go is 1.26.5",
    "sources": ["https://go.dev/doc/devel/release"] }
]
```

This is what a generated line rests on, months later, when the value in the file is wrong and the
question is where it came from:

```shell
jq -r 'select(.searches) | [.file, .slot.instruction, (.searches[].sources[])] | @tsv' ~/.psl/psl.log
jq -r 'select(.searches[]?.error) | [.time, .searches[].error] | @tsv' ~/.psl/psl.log   # searches that failed
```

A search that failed is recorded with its reason rather than dropped: the slot was resolved without
it, and the log is the only place that shows.

Each entry names the `file` it came from, so one log still separates by project:

```shell
jq -r 'select(.file | test("fib")) | .slot.instruction' ~/.psl/psl.log
```

Two things are deliberately absent: your API key, which travels in a header and never in the body, and the bytes of any attached image — an image keeps its media type, but its payload is replaced by a note of its size, `…70 bytes elided…`. The log grows without bound and is never rotated; delete it whenever you like. Living outside your repositories, it is never at risk of being committed.
