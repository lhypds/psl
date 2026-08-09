.psl/psl.log
------------

Every AI request is recorded in `.psl/psl.log`, in the directory you run psl from. One request is one JSON object on one line — prompts and responses span many lines, so a line-per-request keeps the file appendable and greppable:

```json
{
  "time": "2026-08-10T06:18:28+09:00",
  "psl_version": "0.0.2",
  "file": "fib.go.psl",
  "slot": { "line": 3, "column": 2, "instruction": "fill in the iterative loop" },
  "model": {
    "name": "claude-opus-5", "id": "claude-opus-5",
    "base_url": "https://api.anthropic.com", "api": "anthropic",
    "endpoint": "https://api.anthropic.com/v1/messages", "max_tokens": 2048
  },
  "request": { "system": "…", "prompt": "…", "image": { "media_type": "image/png", "bytes": 70 } },
  "response": { "text": "…", "stop_reason": "end_turn" },
  "usage": { "input_tokens": 412, "output_tokens": 37, "total_tokens": 449 },
  "duration_ms": 3841
}
```

Failed requests are logged too, with `error` in place of `response` — which is what makes the log worth having when a slot comes back wrong.

```shell
jq -r 'select(.error) | [.time, .model.name, .error] | @tsv' .psl/psl.log   # what went wrong
jq -s 'map(.usage.total_tokens // 0) | add' .psl/psl.log                     # tokens spent
```

Two things are deliberately absent: your API key, and the bytes of any attached image — an image is recorded by media type and size only. The log grows without bound and is never rotated; delete it whenever you like. Add `.psl/` to your `.gitignore` if you would rather not commit prompts.
