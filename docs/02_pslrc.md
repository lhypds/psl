.pslrc
------

Specify API keys and models. The compiler looks for `.pslrc` in the current directory, then in your home directory.

Each section name is the model name you write in a slot — `[claude-opus-5]` is what makes `:: claude-opus-5> xxx ::` resolve.

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
| `model` | model id sent to the API, when it differs from the section name |
| `api` | wire protocol, `anthropic` or `openai`; inferred from `base_url` otherwise |
| `max_tokens` | output limit for one slot (default 8192) |

`#` and `;` start a comment.
