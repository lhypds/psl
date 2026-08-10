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

`request` is the JSON body the endpoint received, verbatim — not psl's idea of it. Every model is spoken to the same way, so it is always an OpenAI chat completions body: the system prompt is the first message, and an attached image rides in the user message as a data URL. Everything psl composed is in there, and nothing else is: no field exists that the API was not sent. It is recorded for failed requests too, which is what makes a rejected call reproducible:

```shell
jq 'select(.error) | .request' ~/.psl/psl.log                                 # what was sent
jq -r '.request.messages[-1].content | if type == "string" then . else .[-1].text end' ~/.psl/psl.log
```

Each entry names the `file` it came from, so one log still separates by project:

```shell
jq -r 'select(.file | test("fib")) | .slot.instruction' ~/.psl/psl.log
```

Two things are deliberately absent: your API key, which travels in a header and never in the body, and the bytes of any attached image — an image keeps its media type, but its payload is replaced by a note of its size, `…70 bytes elided…`. The log grows without bound and is never rotated; delete it whenever you like. Living outside your repositories, it is never at risk of being committed.
