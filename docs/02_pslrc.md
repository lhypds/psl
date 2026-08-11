.pslrc
------

Specify API keys and models. The compiler looks for `.pslrc` in the current directory, then in your home directory.

```shell
psl config
```

opens that file in your terminal editor — `$VISUAL`, then `$EDITOR`, then whichever of `vim`, `vi` and `nano` is installed. If there is no `.pslrc` yet it creates `~/.pslrc` from the example shipped with psl, readable and writable by you alone, since this is the file API keys go in. The file is parsed when the editor exits, so a mistake in it is reported straight away.

Each section name is the model name you write in a slot — `[claude-opus-5]` is what makes `:: claude-opus-5> xxx ::` resolve. It is also the id sent to the API, so a section is named after the model it configures and nothing inside it renames that.

```text
default_model=claude-opus-5

[gpt-5.6]
base_url=https://api.openai.com
api_key=<your_openai_api_key>

[claude-opus-5]
base_url=https://api.anthropic.com
api_key=<your_anthropic_api_key>
```

If the requested model has no section, the compiler reports an error and exits without modifying the file. The same holds for any failure during a run: the file is rewritten only after the model returns usable output, so a failed run can simply be retried.


Without a .pslrc

`.pslrc` is optional. Whatever it does not configure is taken from the API keys in your environment, checked in this order:

| environment | model used | endpoint |
| --- | --- | --- |
| `OPENAI_API_KEY` | `gpt-5.6` | `https://api.openai.com` |
| `ANTHROPIC_API_KEY` | `claude-opus-5` | `https://api.anthropic.com` |

So this is enough to compile a file:

```shell
export OPENAI_API_KEY=<your_openai_api_key>
psl fib.go.psl
```

The first key found supplies the default model, and each key found also makes its model available by name in a slot — with `ANTHROPIC_API_KEY` exported, `:: claude-opus-5> xxx ::` resolves without any configuration. If neither key is set and no `.pslrc` configures a model, the compiler reports an error and exits.

`.pslrc` always wins where the two overlap: a section you wrote is never overridden by the environment. A section with no `api_key` borrows the one belonging to the provider it talks to, so a file that only redirects `base_url` still needs no secrets in it.

Values may reference environment variables as `${VAR}`, which keeps keys out of the file:

```text
[claude-opus-5]
base_url=https://api.anthropic.com
api_key=${ANTHROPIC_API_KEY}
```

Besides `base_url` and `api_key`, a section accepts:

| key | meaning |
| --- | --- |
| `max_tokens` | output limit for one slot (default 8192) |
| `params` | a JSON object merged into the request body as written |
| `api` | ignored; it used to pick a wire protocol, and there is only one now |

`params` is the way through to whatever an endpoint offers beyond a completion. psl knows what a
request needs — a model, the messages, a cap on the answer — and nothing about what any one endpoint
puts alongside them, so those fields go over as they were typed rather than through a key here for
each:

```text
[qwen3-vl:8b]
base_url=http://127.0.0.1:11434
api_key=ollama
params={"temperature": 0, "reasoning_effort": "none"}
```

It is one JSON object on one line, and numbers are sent as they were written — a `temperature` of
`0` arrives as `0`. The three fields psl builds itself — `model`, `messages`, `max_completion_tokens`
— are refused where they are written: a section that could overwrite them would be deciding what the
compiler is for.

What a given field does is the endpoint's business and not psl's. A reasoning switch that endpoint
does not read is a field it ignores, and a model that thinks whatever it is sent goes on thinking —
some are built that way and have a separate non-reasoning release instead. If the point is a faster
answer, time a slot before and after rather than trusting the switch.

Every endpoint is spoken to identically — `POST <base_url>/v1/chat/completions` with the key as a bearer token, the OpenAI chat completions protocol. Anthropic serves it too, at its own base URL, so a model is configured by URL, key, and id alone; anything else that speaks it works the same way, a local server included.

`#` and `;` start a comment.
