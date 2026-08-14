package lang_test

import (
	"testing"

	"psl/internal/lang"
	"psl/internal/lang/c"
	"psl/internal/lang/csharp"
	"psl/internal/lang/golang"
	"psl/internal/lang/javascript"
	"psl/internal/lang/python"
	"psl/internal/lang/rust"
	"psl/internal/lang/typescript"
	"psl/internal/langtest"
)

// Realistic files with no slot in them at all. Every `::` below is the
// language's own, and psl compiling one of these must report that there is
// nothing left to resolve rather than inventing an instruction out of the
// source. This is the test that a language folder is really finished.
func TestNoSlotInOrdinarySource(t *testing.T) {
	tests := []struct {
		lang *lang.Language
		src  string
	}{
		{golang.Language, `package netx

import (
	"fmt"
	"net"
	"strings"
)

// Listen binds to addr, defaulting to the IPv6 loopback "::1".
func Listen(addr string) (net.Listener, error) {
	if addr == "" {
		addr = "[::1]:0"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split %q: %w", addr, err)
	}
	if strings.Contains(host, "::") {
		host = "[" + host + "]"
	}
	hint := ` + "`dial ::1 first, then ::ffff:0:0`" + `
	_ = hint
	return net.Listen("tcp", net.JoinHostPort(host, port))
}
`},

		{python.Language, `import re

PATTERN = re.compile(r"^[0-9a-f:]+::1$")


def normalise(xs, addr="::1"):
    """Reverse xs and expand the '::' in addr."""
    rev = xs[::-1]
    evens = xs[::2]
    tail = xs[1::]
    strided = xs[1:9:2]
    if "::" in addr:
        addr = addr.replace("::", ":0:")
    return rev, evens, tail, strided, addr
`},

		{c.Language, `#include <stdio.h>
#include <string.h>

/* Print the loopback address, "::1", one char at a time. */
[[maybe_unused]] static int describe(void) {
    const char *addr = "::1";
    const wchar_t *wide = L"::1";
    const char *utf8 = u8"a::b";
    char colon = ':';
    char quote = '\'';
    if (strstr(addr, "::") != NULL) {
        printf("%s %s %c%c\n", addr, utf8, colon, quote);
    }
    return 0;
}
`},

		{csharp.Language, `using System;

namespace App;

public static class Net
{
    // The loopback is "::1", and global::System is the alias qualifier.
    public const string Loopback = "::1";

    public static readonly string Path = @"C:\logs\""::1""";

    public static readonly string Raw = """{"addr": "::1"}""";

    public static string Describe(int port) =>
        $"{global::System.Net.IPAddress.IPv6Loopback}::{port}";
}
`},

		{rust.Language, `use std::collections::HashMap;
use std::fmt::{self, Write};

/// Renders the loopback, ` + "`::1`" + `, through ` + "`<T as fmt::Display>::fmt`" + `.
pub fn render<'a>(map: &'a HashMap<&'a str, u16>) -> String {
    let mut out = String::new();
    let raw = r#"say "::1" here"#;
    let list = Vec::<u16>::new();
    for (k, v) in map {
        write!(out, "{}::{}", k, v).unwrap();
    }
    let _ = (raw, list, <u16 as Default>::default(), '\'');
    out
}
`},

		{javascript.Language, `const RE = /^[a-f0-9:]+::1$/;

export function describe(host, port) {
  const label = ` + "`${host}::${port}`" + `;
  const css = "li::before";
  const ratio = port / 2;
  if (RE.test(host) && host.includes("::")) {
    return ` + "`${label} ${css} ${ratio}`" + `;
  }
  return label;
}
`},

		{typescript.Language, `type Addr = { [key: string]: number };

const RE: RegExp = /^[a-f0-9:]+::1$/;

export function describe(map: Addr, host: string): string {
  const css = "li::before";
  const port = map[host] ?? 0;
  return ` + "`${host}::${port} ${css}`" + `;
}
`},
	}

	for _, tc := range tests {
		t.Run(tc.lang.Name, func(t *testing.T) {
			if got, ok := langtest.FirstSlot(tc.lang, tc.src); ok {
				t.Errorf("%s read %q as a slot in ordinary source:\n%s", tc.lang.Name, got, tc.src)
			}
		})
	}
}
