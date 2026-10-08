#!/usr/bin/env python3
"""Evaluate a few pure functions of the product build's own ``web/src`` TypeScript (harness only).

H10 (upd3): the owner-update driver must show the update card and the
recovery screen the way the product build renders them, not through a frozen
copy of the rules. The card's rules are plain, side-effect-free functions
(``failedUpdateGuidance`` and its helpers, ``parseRecoveryObservation``,
``recoveryFailureGuidanceKey``, ``systemUpdateFailureMessage`` ...). This module
reads those functions from the build's files and evaluates them directly.

It understands a deliberately small, strict subset of TypeScript: ``const``/
``let`` declarations (type annotations skipped), ``if``/``else``, blocks,
``return``, ``throw``, assignments to plain names, and expressions made of
literals, template strings, regular-expression literals, arrays, object
literals (with spread), member access (``.``, ``?.``, ``[]``), calls, ``new``,
``!``, ``typeof``, ``===``/``!==``/``==``/``!=``, relational and ``+``/``-``,
``&&``/``||``/``??``, the ternary and ``as`` casts (skipped). Builtins: the
string, array and RegExp methods these functions use, ``String``,
``Number.isFinite``, ``Array.isArray``, ``Date.parse`` and ``new Date(..)
.toISOString()`` for ISO-8601 UTC instants.

Anything outside the subset raises ``Unsupported``; the driver then records
the card as *not modelled* (unknown), never as a mismatch. No code is executed
other than these interpreted expressions; nothing is imported or evaluated by
Python's ``eval``.

H11 (upd4): the recovery screen (``RecoveryStatus`` in ``RecoveryAccess.tsx``)
is JSX, so the same subset reads JSX expressions too (``tokenize(..., jsx=True)``):
elements, fragments, string and expression attributes (attributes are kept,
never evaluated), text children (JSX whitespace rules, HTML entities) and
``{expression}`` children, plus the TypeScript non-null ``x!``.
``component_return_jsx`` finds the one JSX tree a component returns,
``component_renderings`` also the tree a component keeps in a constant and
shows either plainly or inside one wrapper (``RecoveryStatus`` closed under
``<details>`` during the planned handover: both renderings are read),
``find_elements`` the region the driver reads (``role="status"``) and
``render_region`` evaluates that region with the build's own functions and
returns its visible lines in source order: one line per block element (``p``,
``pre``, headings), inline elements (``span``, ``code``, ``time`` ...) joined
into their line. A component (capitalised tag) or any other element inside the
region is ``Unsupported``: the screen is then *not modelled*, never a mismatch.
"""
from __future__ import annotations

import datetime as dt
import html
import math
import re
from typing import Any


class Unsupported(ValueError):
    """The source uses a construct outside the reviewed subset."""


class JSThrow(Exception):
    """``throw`` inside an evaluated function (the product rejected its input)."""


class _Return(Exception):
    def __init__(self, value: Any) -> None:
        self.value = value


UNDEFINED = None
PUNCTUATORS = sorted(["...", "===", "!==", "=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "+=", "-=",
                      "{", "}", "(", ")", "[", "]", ";", ",", "<", ">", "+", "-", "*", "%", "!", "?", ":",
                      "=", ".", "&", "|", "@", "/", "^", "~"], key=len, reverse=True)
KEYWORDS = {"const", "let", "var", "if", "else", "return", "throw", "function", "new", "typeof", "true", "false",
            "null", "undefined", "as", "export", "import", "type", "readonly"}
NON_NULL_FOLLOWERS = {")", "]", ",", ";", "}", ".", "?.", ":", None}
REGEX_PRECEDERS = {"(", ",", "=", ":", "[", "!", "&&", "||", "??", "?", "{", "}", ";", "return", "===", "!==",
                   "==", "!=", "+", "-", "typeof"}


# -- tokenizer --------------------------------------------------------------------

JSX_PRECEDERS = {"(", ",", "=", ":", "[", "?", "{", "&&", "||", "??", "return", "=>"}


def tokenize(source: str, jsx: bool = False) -> list[tuple[str, Any]]:
    """Tokens of ``source``; with ``jsx`` a JSX element in expression position is one ``("jsx", tree)`` token."""
    tokens, _ = _tokens(source, 0, jsx=jsx, until_brace=False)
    tokens.append(("eof", None))
    return tokens


def _tokens(source: str, i: int, *, jsx: bool, until_brace: bool) -> tuple[list[tuple[str, Any]], int]:
    """Tokens from ``i``; with ``until_brace`` up to the ``}`` closing an already opened ``{`` (returned past it)."""
    tokens: list[tuple[str, Any]] = []
    n = len(source)
    depth = 0
    while i < n:
        c = source[i]
        if until_brace and c == "}" and depth == 0:
            return tokens, i + 1
        if c == "{":
            depth += 1
        elif c == "}":
            depth -= 1
        if (jsx and c == "<" and i + 1 < n and (source[i + 1].isalpha() or source[i + 1] == ">")
                and (not tokens or tokens[-1][0] in ("punct", "kw") and tokens[-1][1] in JSX_PRECEDERS)):
            tree, i = _jsx_element(source, i)
            tokens.append(("jsx", tree))
            continue
        if c.isspace():
            i += 1
            continue
        if source.startswith("//", i):
            end = source.find("\n", i)
            i = n if end < 0 else end
            continue
        if source.startswith("/*", i):
            end = source.find("*/", i + 2)
            if end < 0:
                raise Unsupported("unterminated comment")
            i = end + 2
            continue
        if c in "'\"":
            j, out = i + 1, []
            while j < n and source[j] != c:
                if source[j] == "\\":
                    j += 1
                    out.append({"n": "\n", "t": "\t", "\\": "\\", "'": "'", '"': '"'}.get(source[j], source[j]))
                else:
                    out.append(source[j])
                j += 1
            tokens.append(("str", "".join(out)))
            i = j + 1
            continue
        if c == "`":
            parts, j, buffer = [], i + 1, []
            while j < n and source[j] != "`":
                if source.startswith("${", j):
                    depth, k = 1, j + 2
                    while k < n and depth:
                        depth += {"{": 1, "}": -1}.get(source[k], 0)
                        k += 1
                    parts.append("".join(buffer))
                    buffer = []
                    parts.append(("expr", source[j + 2:k - 1]))
                    j = k
                    continue
                if source[j] == "\\":
                    j += 1
                buffer.append(source[j])
                j += 1
            parts.append("".join(buffer))
            tokens.append(("template", parts))
            i = j + 1
            continue
        if c == "/" and (not tokens or tokens[-1][0] in ("punct", "kw") and tokens[-1][1] in REGEX_PRECEDERS):
            j, in_class = i + 1, False
            while j < n:
                ch = source[j]
                if ch == "\\":
                    j += 2
                    continue
                if ch == "[":
                    in_class = True
                elif ch == "]":
                    in_class = False
                elif ch == "/" and not in_class:
                    break
                j += 1
            k = j + 1
            while k < n and source[k].isalpha():
                k += 1
            tokens.append(("regex", (source[i + 1:j], source[j + 1:k])))
            i = k
            continue
        if c.isdigit():
            m = re.match(r"\d+(\.\d+)?", source[i:])
            tokens.append(("num", float(m.group(0)) if "." in m.group(0) else int(m.group(0))))
            i += len(m.group(0))
            continue
        if c.isalpha() or c in "_$":
            m = re.match(r"[A-Za-z_$][A-Za-z0-9_$]*", source[i:])
            word = m.group(0)
            tokens.append(("kw" if word in KEYWORDS else "name", word))
            i += len(word)
            continue
        for p in PUNCTUATORS:
            if source.startswith(p, i):
                tokens.append(("punct", p))
                i += len(p)
                break
        else:
            raise Unsupported(f"unexpected character {c!r}")
    if until_brace:
        raise Unsupported("unterminated JSX expression")
    return tokens, i


JSX_NAME = re.compile(r"[A-Za-z_$][A-Za-z0-9_$.:-]*")


def _jsx_element(source: str, i: int) -> tuple[dict, int]:
    """One JSX element or fragment starting at ``source[i] == "<"``; returns its tree and the index after it.

    Tree: ``{"tag": name | None (fragment), "attrs": {name: ("str", text) | ("tokens", [...]) | ("bool", True)},
    "children": [("text", raw) | ("expr", [tokens]) | ("element", tree)]}``.
    """
    n = len(source)
    i += 1
    attrs: dict[str, tuple] = {}
    if source[i] == ">":
        tag, i = None, i + 1
    else:
        match = JSX_NAME.match(source, i)
        if not match:
            raise Unsupported("JSX tag name")
        tag, i = match.group(0), match.end()
        while True:
            while i < n and source[i].isspace():
                i += 1
            if source.startswith("/>", i):
                return {"tag": tag, "attrs": attrs, "children": []}, i + 2
            if i >= n:
                raise Unsupported("unterminated JSX tag")
            if source[i] == ">":
                i += 1
                break
            if source[i] == "{":
                tokens, i = _tokens(source, i + 1, jsx=True, until_brace=True)
                attrs["..." + str(len(attrs))] = ("tokens", tokens)
                continue
            match = JSX_NAME.match(source, i)
            if not match:
                raise Unsupported(f"JSX attribute at {source[i:i + 12]!r}")
            name, i = match.group(0), match.end()
            while i < n and source[i].isspace():
                i += 1
            if i < n and source[i] == "=":
                i += 1
                while i < n and source[i].isspace():
                    i += 1
                if i < n and source[i] in "'\"":
                    end = source.find(source[i], i + 1)
                    if end < 0:
                        raise Unsupported("unterminated JSX attribute string")
                    attrs[name] = ("str", html.unescape(source[i + 1:end]))
                    i = end + 1
                elif i < n and source[i] == "{":
                    tokens, i = _tokens(source, i + 1, jsx=True, until_brace=True)
                    attrs[name] = ("tokens", tokens)
                else:
                    raise Unsupported(f"JSX attribute value of {name}")
            else:
                attrs[name] = ("bool", True)
    children: list[tuple] = []
    while True:
        if i >= n:
            raise Unsupported(f"unclosed JSX element {tag}")
        if source.startswith("</", i):
            j = i + 2
            match = JSX_NAME.match(source, j)
            closing = match.group(0) if match else None
            j = match.end() if match else j
            while j < n and source[j].isspace():
                j += 1
            if closing != tag or j >= n or source[j] != ">":
                raise Unsupported(f"JSX closing tag {closing!r} for {tag!r}")
            return {"tag": tag, "attrs": attrs, "children": children}, j + 1
        if source[i] == "<":
            child, i = _jsx_element(source, i)
            children.append(("element", child))
            continue
        if source[i] == "{":
            tokens, i = _tokens(source, i + 1, jsx=True, until_brace=True)
            children.append(("expr", tokens))
            continue
        j = i
        while j < n and source[j] not in "<{":
            j += 1
        children.append(("text", source[i:j]))
        i = j


def clean_jsx_text(raw: str) -> str:
    """JSX text as React renders it (Babel's rule): lines trimmed at their inner edges, blank lines dropped,
    the remaining lines joined by one space; HTML entities decoded."""
    lines = re.split(r"\r\n|\n|\r", raw)
    last_non_empty = max((index for index, line in enumerate(lines) if line.strip(" \t")), default=-1)
    out = []
    for index, line in enumerate(lines):
        text = line.replace("\t", " ")
        if index != 0:
            text = text.lstrip(" ")
        if index != len(lines) - 1:
            text = text.rstrip(" ")
        if text:
            out.append(text if index == last_non_empty else text + " ")
    return html.unescape("".join(out))


# -- parser -------------------------------------------------------------------------

class Parser:
    def __init__(self, tokens: list) -> None:
        self.t, self.i = tokens, 0

    def peek(self, offset: int = 0):
        return self.t[min(self.i + offset, len(self.t) - 1)]

    def at(self, value: Any, kind: str | None = None) -> bool:
        k, v = self.peek()
        return v == value and (kind is None or k == kind)

    def eat(self, value: Any = None, kind: str | None = None):
        k, v = self.peek()
        if (value is not None and v != value) or (kind is not None and k != kind):
            raise Unsupported(f"expected {value or kind}, found {v!r}")
        self.i += 1
        return v

    def skip_type(self, stops: set[str]) -> None:
        """Skip a type annotation or ``as`` target up to a depth-0 stop token."""
        depth = 0
        while True:
            k, v = self.peek()
            if k == "eof":
                return
            if depth == 0 and k == "punct" and v in stops:
                return
            if k == "punct" and v in ("(", "[", "{", "<"):
                depth += 1
            elif k == "punct" and v in (")", "]", "}", ">"):
                if depth == 0:
                    return
                depth -= 1
            elif k == "punct" and v == "=>" and depth == 0 and "=>" in stops:
                return
            self.i += 1

    # statements
    def block(self) -> list:
        self.eat("{")
        body = []
        while not self.at("}"):
            body.append(self.statement())
        self.eat("}")
        return body

    def statement(self):
        k, v = self.peek()
        if v == "{" and k == "punct":
            return ("block", self.block())
        if k == "kw" and v in ("const", "let", "var"):
            self.i += 1
            decls = []
            while True:
                name = self.eat(kind="name")
                if self.at(":"):
                    self.eat(":")
                    self.skip_type({"=", ",", ";"})
                init = None
                if self.at("="):
                    self.eat("=")
                    init = self.expression()
                decls.append((name, init))
                if not self.at(","):
                    break
                self.eat(",")
            self.optional_semicolon()
            return ("decl", decls)
        if k == "kw" and v == "if":
            self.i += 1
            self.eat("(")
            test = self.expression()
            self.eat(")")
            then = self.statement()
            other = None
            if self.at("else", "kw"):
                self.eat("else")
                other = self.statement()
            return ("if", test, then, other)
        if k == "kw" and v == "return":
            self.i += 1
            value = None if self.at(";") or self.at("}") else self.expression()
            self.optional_semicolon()
            return ("return", value)
        if k == "kw" and v == "throw":
            self.i += 1
            value = self.expression()
            self.optional_semicolon()
            return ("throw", value)
        expr = self.expression()
        self.optional_semicolon()
        return ("expr", expr)

    def optional_semicolon(self) -> None:
        if self.at(";"):
            self.eat(";")

    # expressions
    def expression(self):
        left = self.conditional()
        if self.at("=", "punct"):
            if left[0] != "name":
                raise Unsupported("assignment to a non-name")
            self.eat("=")
            return ("assign", left[1], self.expression())
        return left

    def conditional(self):
        test = self.logical()
        if self.at("?", "punct"):
            self.eat("?")
            then = self.expression()
            self.eat(":")
            other = self.expression()
            return ("cond", test, then, other)
        return test

    def logical(self):
        left = self.logical_and()
        while self.peek()[1] in ("||", "??") and self.peek()[0] == "punct":
            op = self.eat()
            left = (op, left, self.logical_and())
        return left

    def logical_and(self):
        left = self.equality()
        while self.at("&&", "punct"):
            self.eat()
            left = ("&&", left, self.equality())
        return left

    def equality(self):
        left = self.relational()
        while self.peek()[1] in ("===", "!==", "==", "!=") and self.peek()[0] == "punct":
            op = self.eat()
            left = ("binary", op, left, self.relational())
        return left

    def relational(self):
        left = self.additive()
        while self.peek()[1] in ("<", ">", "<=", ">=") and self.peek()[0] == "punct":
            op = self.eat()
            left = ("binary", op, left, self.additive())
        return left

    def additive(self):
        left = self.unary()
        while self.peek()[1] in ("+", "-") and self.peek()[0] == "punct":
            op = self.eat()
            left = ("binary", op, left, self.unary())
        return left

    def unary(self):
        k, v = self.peek()
        if k == "punct" and v == "!":
            self.eat()
            return ("not", self.unary())
        if k == "punct" and v == "-":
            self.eat()
            return ("neg", self.unary())
        if k == "kw" and v == "typeof":
            self.eat()
            return ("typeof", self.unary())
        expr = self.postfix()
        while self.at("as", "kw"):
            self.eat()
            self.skip_type({")", ",", ";", "]", "}", "?", ":", "&&", "||", "??", "===", "!==", "=="})
        return expr

    def postfix(self):
        expr = self.primary()
        while True:
            k, v = self.peek()
            if k == "punct" and v == ".":
                self.eat()
                expr = ("member", expr, self.eat_name(), False)
            elif k == "punct" and v == "?.":
                self.eat()
                if self.at("["):
                    self.eat("[")
                    index = self.expression()
                    self.eat("]")
                    expr = ("index", expr, index, True)
                elif self.at("("):
                    raise Unsupported("optional call")
                else:
                    expr = ("member", expr, self.eat_name(), True)
            elif k == "punct" and v == "[":
                self.eat()
                index = self.expression()
                self.eat("]")
                expr = ("index", expr, index, False)
            elif k == "punct" and v == "(":
                expr = ("call", expr, self.arguments())
            elif (k == "punct" and v == "!" and self.peek(1)[0] in ("punct", "eof")
                  and self.peek(1)[1] in NON_NULL_FOLLOWERS):
                self.eat()        # TypeScript non-null assertion ``x!``: no runtime effect
            else:
                return expr

    def eat_name(self) -> str:
        k, v = self.peek()
        if k not in ("name", "kw"):
            raise Unsupported(f"expected a property name, found {v!r}")
        self.i += 1
        return v

    def arguments(self) -> list:
        self.eat("(")
        args = []
        while not self.at(")"):
            args.append(self.expression())
            if not self.at(")"):
                self.eat(",")
        self.eat(")")
        return args

    def primary(self):
        k, v = self.peek()
        if k == "str":
            self.i += 1
            return ("lit", v)
        if k == "num":
            self.i += 1
            return ("lit", v)
        if k == "template":
            self.i += 1
            return ("template", [part if isinstance(part, str) else Parser(tokenize(part[1])).expression()
                                 for part in v])
        if k == "regex":
            self.i += 1
            return ("regex", v[0], v[1])
        if k == "jsx":
            self.i += 1
            return ("jsx", v)
        if k == "kw" and v in ("true", "false"):
            self.i += 1
            return ("lit", v == "true")
        if k == "kw" and v in ("null", "undefined"):
            self.i += 1
            return ("lit", None)
        if k == "kw" and v == "new":
            self.i += 1
            name = self.eat(kind="name")
            args = self.arguments() if self.at("(") else []
            return ("new", name, args)
        if k == "name":
            self.i += 1
            return ("name", v)
        if k == "punct" and v == "(":
            self.eat("(")
            expr = self.expression()
            self.eat(")")
            if self.at("=>"):
                raise Unsupported("arrow function")
            return expr
        if k == "punct" and v == "[":
            self.eat("[")
            items = []
            while not self.at("]"):
                if self.at("..."):
                    self.eat()
                    items.append(("spread", self.expression()))
                else:
                    items.append(self.expression())
                if not self.at("]"):
                    self.eat(",")
            self.eat("]")
            return ("array", items)
        if k == "punct" and v == "{":
            self.eat("{")
            props = []
            while not self.at("}"):
                if self.at("..."):
                    self.eat()
                    props.append(("spread", self.expression()))
                else:
                    kk, key = self.peek()
                    if kk not in ("name", "kw", "str"):
                        raise Unsupported(f"object key {key!r}")
                    self.i += 1
                    if self.at(":"):
                        self.eat(":")
                        props.append(("prop", key, self.expression()))
                    else:
                        props.append(("prop", key, ("name", key)))
                if not self.at("}"):
                    self.eat(",")
            self.eat("}")
            return ("object", props)
        raise Unsupported(f"unexpected token {v!r}")


# -- values and builtins --------------------------------------------------------------

class JSRegExp:
    def __init__(self, pattern: str, flags: str) -> None:
        if set(flags) - set("gi"):
            raise Unsupported(f"regex flags {flags!r}")
        self.source, self.flags = pattern, flags
        python = pattern.replace("(?<", "(?P<") if "(?<" in pattern and "(?<=" not in pattern and "(?<!" not in pattern \
            else pattern
        python = re.sub(r"(?<!\\)\$$", r"\\Z", python)
        self.regex = re.compile(python, re.I if "i" in flags else 0)


class Function:
    def __init__(self, name: str, params: list[str], body: list, module: "Module") -> None:
        self.name, self.params, self.body, self.module = name, params, body, module


def truthy(value: Any) -> bool:
    if value is None or value is False:
        return False
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return value != 0 and not (isinstance(value, float) and math.isnan(value))
    if isinstance(value, str):
        return value != ""
    return True


def to_string(value: Any) -> str:
    if value is None:
        return "undefined"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, float) and value.is_integer():
        return str(int(value))
    if isinstance(value, list):
        return ",".join("" if v is None else to_string(v) for v in value)
    return str(value)


def strict_equal(a: Any, b: Any) -> bool:
    if isinstance(a, bool) or isinstance(b, bool):
        return type(a) is type(b) and a == b
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return a == b
    if type(a) is not type(b):
        return False
    if isinstance(a, (list, dict)):
        return a is b
    return a == b


ISO_RE = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d{1,3})?Z\Z")


def date_parse(value: Any) -> float:
    match = ISO_RE.match(value) if isinstance(value, str) else None
    if not match:
        return float("nan")
    try:
        moment = dt.datetime(*(int(match.group(i)) for i in range(1, 7)), tzinfo=dt.timezone.utc)
    except ValueError:
        return float("nan")
    millis = int((match.group(7) or ".0")[1:].ljust(3, "0"))
    return moment.timestamp() * 1000 + millis


class JSDate:
    def __init__(self, value: Any) -> None:
        self.ms = date_parse(value)

    def to_iso(self) -> str:
        if math.isnan(self.ms):
            raise JSThrow("RangeError: Invalid time value")
        moment = dt.datetime.fromtimestamp(self.ms / 1000, dt.timezone.utc)
        return moment.strftime("%Y-%m-%dT%H:%M:%S.") + f"{int(round(self.ms % 1000)):03d}Z"


def js_replace(subject: str, pattern: Any, replacement: Any) -> str:
    if not isinstance(replacement, str):
        raise Unsupported("replace with a function")
    if isinstance(pattern, JSRegExp):
        python = re.sub(r"\$(\d)", r"\\g<\1>", replacement.replace("\\", "\\\\"))
        return pattern.regex.sub(python, subject, count=0 if "g" in pattern.flags else 1)
    return subject.replace(to_string(pattern), replacement, 1)


def member(obj: Any, name: str) -> Any:
    if isinstance(obj, dict):
        return obj.get(name)
    if isinstance(obj, str):
        if name == "length":
            return len(obj)
        methods = {"trim": lambda: obj.strip(), "includes": lambda s: to_string(s) in obj,
                   "startsWith": lambda s: obj.startswith(to_string(s)), "endsWith": lambda s: obj.endswith(to_string(s)),
                   "replace": lambda p, r: js_replace(obj, p, r), "toLowerCase": lambda: obj.lower(),
                   "toUpperCase": lambda: obj.upper()}
        if name in methods:
            return methods[name]
    if isinstance(obj, list):
        if name == "length":
            return len(obj)
        methods = {"includes": lambda v: any(strict_equal(x, v) for x in obj),
                   "indexOf": lambda v: next((i for i, x in enumerate(obj) if strict_equal(x, v)), -1),
                   "join": lambda sep=",": sep.join(to_string(x) for x in obj),
                   "push": lambda *values: (obj.extend(values), len(obj))[1]}
        if name in methods:
            return methods[name]
        return None
    if isinstance(obj, JSRegExp):
        if name == "test":
            return lambda s: obj.regex.search(to_string(s)) is not None
        if name == "exec":
            def exec_(s):
                m = obj.regex.search(to_string(s))
                return None if m is None else [m.group(0)] + list(m.groups())
            return exec_
        if name in ("source", "flags"):
            return getattr(obj, name)
    if isinstance(obj, JSDate) and name == "toISOString":
        return obj.to_iso
    if isinstance(obj, Builtin):
        return obj.members.get(name)
    raise Unsupported(f"property {name!r} of {type(obj).__name__}")


class Builtin:
    def __init__(self, members: dict | None = None, call=None) -> None:
        self.members, self.call = members or {}, call


GLOBALS = {
    "String": Builtin(call=lambda v=None: to_string(v)),
    "Number": Builtin({"isFinite": lambda v: isinstance(v, (int, float)) and not isinstance(v, bool)
                       and math.isfinite(v)}),
    "Array": Builtin({"isArray": lambda v: isinstance(v, list)}),
    "Date": Builtin({"parse": date_parse}),
    "Error": Builtin(call=lambda message="": {"message": message}),
}


# -- evaluator -----------------------------------------------------------------------

class Module:
    """The top-level constants and functions of one or more product source files."""

    def __init__(self, sources: dict[str, str]) -> None:
        self.functions: dict[str, Function] = {}
        self.constants: dict[str, Any] = {}
        self.pending: dict[str, Any] = {}
        for name, text in sources.items():
            self._scan(text)

    def _scan(self, text: str) -> None:
        """Top-level ``function NAME(...)`` and ``const NAME = ...;`` of one file, found on its tokens (so comments,
        strings, templates and regular expressions never unbalance the scan)."""
        tokens = tokenize(text)
        i, depth = 0, 0
        while tokens[i][0] != "eof":
            kind, value = tokens[i]
            if kind == "punct" and value in ("{", "(", "["):
                depth += 1
            elif kind == "punct" and value in ("}", ")", "]"):
                depth -= 1
            elif depth == 0 and kind == "kw" and value == "function" and tokens[i + 1][0] == "name":
                name = tokens[i + 1][1]
                close = self._close(tokens, i + 2)
                params = self._param_names(tokens[i + 3:close])
                body = self._body_start(tokens, close)
                end = self._close(tokens, body)
                self.functions[name] = ("tokens", params, tokens[body:end + 1] + [("eof", None)])
                i = end + 1
                continue
            elif depth == 0 and kind == "kw" and value == "const" and tokens[i + 1][0] == "name":
                name = tokens[i + 1][1]
                j = i + 2
                while tokens[j][0] != "eof" and tokens[j] != ("punct", "="):
                    j += 1
                k, inner = j + 1, 0
                while tokens[k][0] != "eof" and not (inner == 0 and tokens[k] == ("punct", ";")):
                    if tokens[k][0] == "punct" and tokens[k][1] in ("{", "(", "["):
                        inner += 1
                    elif tokens[k][0] == "punct" and tokens[k][1] in ("}", ")", "]"):
                        inner -= 1
                    k += 1
                self.pending[name] = tokens[j + 1:k] + [("eof", None)]
                i = k
                continue
            i += 1

    @classmethod
    def _body_start(cls, tokens: list, close: int) -> int:
        """Index of a function body's ``{`` after its parameter list, past a return type annotation (which may
        itself be an object type such as ``{ record: X; unavailable: boolean }``)."""
        body = close + 1
        if tokens[body] == ("punct", ":"):
            end = cls._type_end(tokens, body + 1)
            if end is not None and tokens[end] == ("punct", "{"):
                return end
        while tokens[body][0] != "eof" and tokens[body] != ("punct", "{"):
            body += 1
        return body

    @classmethod
    def _type_end(cls, tokens: list, i: int) -> int | None:
        """Index after one TypeScript type at ``i``: unions and intersections of names (dotted, generic), literal,
        object, tuple, parenthesised and function types, array suffixes and ``x is T`` predicates; ``None`` when
        the type is not of that shape."""
        try:
            while True:
                kind, value = tokens[i]
                if (kind == "kw" and value in ("readonly", "typeof")) or (kind == "name" and value == "keyof"):
                    i += 1
                    continue
                if kind == "punct" and value in ("{", "["):
                    i = cls._close(tokens, i) + 1
                elif kind == "punct" and value == "(":
                    i = cls._close(tokens, i) + 1
                    if tokens[i] == ("punct", "=>"):
                        i += 1
                        continue
                elif kind in ("name", "str", "num") or (kind == "kw" and value in ("null", "undefined", "true",
                                                                                      "false")):
                    i += 1
                    while tokens[i] == ("punct", ".") and tokens[i + 1][0] == "name":
                        i += 2
                    if tokens[i] == ("punct", "<"):
                        depth = 0
                        while True:
                            k, v = tokens[i]
                            if k == "eof":
                                return None
                            if k == "punct" and v == "<":
                                depth += 1
                            elif k == "punct" and v == ">":
                                depth -= 1
                            i += 1
                            if depth == 0:
                                break
                    if tokens[i] == ("name", "is"):
                        i += 1
                        continue
                else:
                    return None
                while tokens[i] == ("punct", "[") and tokens[i + 1] == ("punct", "]"):
                    i += 2
                if tokens[i][0] == "punct" and tokens[i][1] in ("|", "&"):
                    i += 1
                    continue
                return i
        except (IndexError, Unsupported):
            return None

    @staticmethod
    def _close(tokens: list, start: int) -> int:
        """Index of the bracket closing the one at ``start``."""
        pairs = {"(": ")", "{": "}", "[": "]"}
        opener = tokens[start][1]
        if tokens[start][0] != "punct" or opener not in pairs:
            raise Unsupported("expected an opening bracket")
        depth = 0
        for index in range(start, len(tokens)):
            kind, value = tokens[index]
            if kind == "punct" and value in pairs:
                depth += 1
            elif kind == "punct" and value in pairs.values():
                depth -= 1
                if depth == 0:
                    return index
        raise Unsupported("unbalanced brackets")

    @staticmethod
    def _param_names(tokens: list) -> list[str]:
        names, depth, expect = [], 0, True
        for kind, value in tokens:
            if kind == "punct" and value in ("(", "[", "{", "<"):
                depth += 1
            elif kind == "punct" and value in (")", "]", "}", ">"):
                depth -= 1
            elif kind == "punct" and value == "," and depth == 0:
                expect = True
            elif expect and depth == 0 and kind == "name":
                names.append(value)
                expect = False
        return names

    def function(self, name: str) -> Function:
        value = self.functions.get(name)
        if value is None:
            raise Unsupported(f"function {name} not found in the build's sources")
        if isinstance(value, tuple):
            _, params, body = value
            value = Function(name, params, Parser(body).block(), self)
            self.functions[name] = value
        return value

    def constant(self, name: str) -> Any:
        if name not in self.constants:
            if name not in self.pending:
                raise KeyError(name)
            self.constants[name] = evaluate(Parser(self.pending[name]).expression(), {}, self)
        return self.constants[name]

    def call(self, name: str, *args: Any) -> Any:
        return call_function(self.function(name), list(args))


def call_function(function: Function, args: list) -> Any:
    scope = {name: (args[i] if i < len(args) else None) for i, name in enumerate(function.params)}
    try:
        run_block(function.body, [scope], function.module)
    except _Return as result:
        return result.value
    return None


def lookup(name: str, scopes: list[dict], module: Module) -> Any:
    for scope in reversed(scopes):
        if name in scope:
            return scope[name]
    if name in module.functions:
        return module.function(name)
    try:
        return module.constant(name)
    except KeyError:
        pass
    if name in GLOBALS:
        return GLOBALS[name]
    raise Unsupported(f"unknown name {name!r}")


def run_block(statements: list, scopes: list[dict], module: Module) -> None:
    for statement in statements:
        run(statement, scopes, module)


def run(statement, scopes: list[dict], module: Module) -> None:
    kind = statement[0]
    if kind == "decl":
        for name, init in statement[1]:
            scopes[-1][name] = None if init is None else evaluate(init, scopes, module)
    elif kind == "expr":
        evaluate(statement[1], scopes, module)
    elif kind == "if":
        if truthy(evaluate(statement[1], scopes, module)):
            run(statement[2], scopes, module)
        elif statement[3] is not None:
            run(statement[3], scopes, module)
    elif kind == "block":
        run_block(statement[1], scopes + [{}], module)
    elif kind == "return":
        raise _Return(None if statement[1] is None else evaluate(statement[1], scopes, module))
    elif kind == "throw":
        raise JSThrow(to_string((evaluate(statement[1], scopes, module) or {}).get("message")))
    else:
        raise Unsupported(f"statement {kind}")


def evaluate(node, scopes: list[dict], module: Module) -> Any:
    kind = node[0]
    if kind == "lit":
        return node[1]
    if kind == "name":
        return lookup(node[1], scopes, module)
    if kind == "template":
        return "".join(part if isinstance(part, str) else to_string(evaluate(part, scopes, module)) for part in node[1])
    if kind == "regex":
        return JSRegExp(node[1], node[2])
    if kind == "array":
        out = []
        for item in node[1]:
            if item[0] == "spread":
                out.extend(evaluate(item[1], scopes, module) or [])
            else:
                out.append(evaluate(item, scopes, module))
        return out
    if kind == "object":
        out: dict = {}
        for prop in node[1]:
            if prop[0] == "spread":
                out.update(evaluate(prop[1], scopes, module) or {})
            else:
                out[prop[1]] = evaluate(prop[2], scopes, module)
        return out
    if kind == "assign":
        value = evaluate(node[2], scopes, module)
        for scope in reversed(scopes):
            if node[1] in scope:
                scope[node[1]] = value
                return value
        raise Unsupported(f"assignment to undeclared {node[1]!r}")
    if kind == "cond":
        return evaluate(node[2] if truthy(evaluate(node[1], scopes, module)) else node[3], scopes, module)
    if kind == "&&":
        left = evaluate(node[1], scopes, module)
        return evaluate(node[2], scopes, module) if truthy(left) else left
    if kind == "||":
        left = evaluate(node[1], scopes, module)
        return left if truthy(left) else evaluate(node[2], scopes, module)
    if kind == "??":
        left = evaluate(node[1], scopes, module)
        return left if left is not None else evaluate(node[2], scopes, module)
    if kind == "not":
        return not truthy(evaluate(node[1], scopes, module))
    if kind == "neg":
        return -evaluate(node[1], scopes, module)
    if kind == "typeof":
        value = evaluate(node[1], scopes, module)
        return ("undefined" if value is None else "boolean" if isinstance(value, bool) else "number"
                if isinstance(value, (int, float)) else "string" if isinstance(value, str) else "object")
    if kind == "binary":
        op, left, right = node[1], evaluate(node[2], scopes, module), evaluate(node[3], scopes, module)
        if op in ("===", "=="):
            return strict_equal(left, right)
        if op in ("!==", "!="):
            return not strict_equal(left, right)
        if op == "+":
            if isinstance(left, str) or isinstance(right, str):
                return to_string(left) + to_string(right)
            return left + right
        if op == "-":
            return left - right
        if left is None or right is None:
            return False
        return {"<": left < right, ">": left > right, "<=": left <= right, ">=": left >= right}[op]
    if kind == "member":
        obj = evaluate(node[1], scopes, module)
        if obj is None:
            if node[3]:
                return None
            raise JSThrow(f"TypeError: cannot read {node[2]} of undefined")
        return member(obj, node[2])
    if kind == "index":
        obj = evaluate(node[1], scopes, module)
        if obj is None:
            if node[3]:
                return None
            raise JSThrow("TypeError: cannot index undefined")
        key = evaluate(node[2], scopes, module)
        if isinstance(obj, list) and isinstance(key, (int, float)) and not isinstance(key, bool):
            return obj[int(key)] if 0 <= int(key) < len(obj) else None
        if isinstance(obj, dict):
            return obj.get(to_string(key))
        raise Unsupported("index of a non-object")
    if kind == "call":
        callee = evaluate(node[1], scopes, module)
        if callee is None and node[1][0] in ("member", "index") and node[1][3]:
            return None      # a?.b(c): the optional chain short-circuits the call
        args = [evaluate(arg, scopes, module) for arg in node[2]]
        if isinstance(callee, Function):
            return call_function(callee, args)
        if isinstance(callee, Builtin) and callee.call:
            return callee.call(*args)
        if callable(callee):
            return callee(*args)
        raise Unsupported("call of a non-function")
    if kind == "new":
        if node[1] == "Date":
            return JSDate(evaluate(node[2][0], scopes, module) if node[2] else None)
        if node[1] == "Error":
            return {"message": evaluate(node[2][0], scopes, module) if node[2] else ""}
        raise Unsupported(f"new {node[1]}")
    if kind == "jsx":
        return render_element(node[1], scopes, module)
    raise Unsupported(f"expression {kind}")


# -- JSX (H11: the recovery screen) ---------------------------------------------------------

class JSXNode:
    """A rendered element: its tag, its string attributes and its evaluated children."""

    def __init__(self, tag: str | None, attrs: dict, children: list) -> None:
        self.tag, self.attrs, self.children = tag, attrs, children


BLOCK_TAGS = frozenset({"p", "pre", "h1", "h2", "h3", "h4", "li", "dt", "dd"})
CONTAINER_TAGS = frozenset({None, "div", "section", "ul", "ol", "dl", "main", "header", "footer", "article", "nav"})
INLINE_TAGS = frozenset({"span", "code", "time", "strong", "em", "b", "i", "kbd", "small", "abbr", "a"})


def _expression_tokens(tokens: list) -> Any:
    parser = Parser(list(tokens) + [("eof", None)])
    expr = parser.expression()
    if parser.peek()[0] != "eof":
        raise Unsupported(f"JSX expression continues after {parser.peek()[1]!r}")
    return expr


def render_element(tree: dict, scopes: list[dict], module: "Module") -> JSXNode:
    """Evaluate one JSX tree in ``scopes``: text cleaned, ``{expr}`` children evaluated (in source order),
    attributes kept only when they are strings (expression attributes are never evaluated)."""
    children: list = []
    for kind, value in tree["children"]:
        if kind == "text":
            text = clean_jsx_text(value)
            if text:
                children.append(text)
        elif kind == "element":
            children.append(render_element(value, scopes, module))
        elif value:                         # ``{/* comment */}`` has no tokens and renders nothing
            children.append(evaluate(_expression_tokens(value), scopes, module))
    attrs = {name: value for name, (kind, value) in tree["attrs"].items() if kind == "str"}
    return JSXNode(tree["tag"], attrs, children)


def _flatten(value: Any) -> list[tuple[str, str]]:
    """React's output of one child as ("inline", text) and ("block", text) parts."""
    if value is None or isinstance(value, bool) or value == "":
        return []
    if isinstance(value, (str, int, float)):
        return [("inline", to_string(value))]
    if isinstance(value, list):
        return [part for item in value for part in _flatten(item)]
    if not isinstance(value, JSXNode):
        raise Unsupported(f"JSX child of type {type(value).__name__}")
    tag = value.tag
    if tag is not None and tag[:1].isupper():
        raise Unsupported(f"component <{tag}> inside the rendered region")
    parts = [part for child in value.children for part in _flatten(child)]
    if tag in CONTAINER_TAGS:
        return parts
    if any(kind == "block" for kind, _ in parts):
        raise Unsupported(f"block content inside <{tag}>")
    text = "".join(text for _, text in parts)
    if tag in INLINE_TAGS:
        return [("inline", text)]
    if tag in BLOCK_TAGS:
        return [("block", text)]
    raise Unsupported(f"element <{tag}> inside the rendered region")


def jsx_lines(value: Any) -> list[str]:
    """The visible lines of a rendered value: each block element one line, adjacent inline content one line."""
    lines: list[str] = []
    inline: list[str] = []
    for kind, text in _flatten(value):
        if kind == "inline":
            inline.append(text)
            continue
        if inline:
            lines.append("".join(inline))
            inline = []
        lines.append(text)
    if inline:
        lines.append("".join(inline))
    return [line.strip() for line in lines if line.strip()]


def _component_top_level(source: str, name: str) -> tuple[list[dict], list[list], dict[str, dict]]:
    """The top level of the body of ``function NAME`` in a ``.tsx`` source.

    Returns the JSX trees it returns directly (``return <...>``), the tokens of every other ``return`` (up to its
    ``;``) and ``{X: tree}`` for every ``const X = <...>;``. Nested blocks, calls and callbacks are not read.
    """
    tokens = tokenize(source, jsx=True)
    starts = [i for i in range(len(tokens) - 1) if tokens[i] == ("kw", "function") and tokens[i + 1] == ("name", name)]
    if len(starts) != 1:
        raise Unsupported(f"component {name} found {len(starts)} times")
    close = Module._close(tokens, starts[0] + 2)
    body = close + 1
    while tokens[body][0] != "eof" and tokens[body] != ("punct", "{"):
        body += 1
    end = Module._close(tokens, body)
    found, others, constants, depth = [], [], {}, 0
    for index in range(body + 1, end):
        kind, value = tokens[index]
        if kind == "punct" and value in ("{", "(", "["):
            depth += 1
        elif kind == "punct" and value in ("}", ")", "]"):
            depth -= 1
        elif depth == 0 and (kind, value) == ("kw", "return"):
            if tokens[index + 1][0] == "jsx":
                found.append(tokens[index + 1][1])
                continue
            stop, inner = index + 1, 0
            while stop < end and not (inner == 0 and tokens[stop] == ("punct", ";")):
                if tokens[stop][0] == "punct" and tokens[stop][1] in ("{", "(", "["):
                    inner += 1
                elif tokens[stop][0] == "punct" and tokens[stop][1] in ("}", ")", "]"):
                    inner -= 1
                stop += 1
            others.append(tokens[index + 1:stop])
        elif (depth == 0 and (kind, value) == ("kw", "const") and index + 4 < end and tokens[index + 1][0] == "name"
              and tokens[index + 2] == ("punct", "=") and tokens[index + 3][0] == "jsx"
              and tokens[index + 4] == ("punct", ";")):
            constants[tokens[index + 1][1]] = tokens[index + 3][1]
    return found, others, constants


def component_return_jsx(source: str, name: str) -> dict:
    """The JSX tree returned by the top-level ``function NAME`` of a ``.tsx`` source (its one ``return <...>``)."""
    found, _, _ = _component_top_level(source, name)
    if len(found) != 1:
        raise Unsupported(f"component {name} returns JSX {len(found)} times at its top level")
    return found[0]


def _wrapped_constant(run: list, constants: dict[str, dict]) -> tuple[dict, dict] | None:
    """``CONDITION ? <wrapper>...{X}...</wrapper> : X`` (either order) for a constant tree X.

    Returns ``(X, wrapper)`` where the wrapper's own ``{X}`` child (exactly one, a direct child) is replaced by
    X's tree itself, so an element found in both is the same object. Anything else is ``None``.
    """
    marks = [i for i, token in enumerate(run) if token == ("punct", "?")]
    if len(marks) != 1 or marks[0] == 0 or len(run) != marks[0] + 4 or run[marks[0] + 2] != ("punct", ":"):
        return None
    if any(kind == "jsx" for kind, _ in run[:marks[0]]):
        return None
    first, second = run[marks[0] + 1], run[marks[0] + 3]
    for wrapper, constant in ((first, second), (second, first)):
        if wrapper[0] != "jsx" or constant[0] != "name" or constant[1] not in constants:
            continue
        children = list(wrapper[1]["children"])
        places = [i for i, child in enumerate(children) if child[0] == "expr" and child[1] == [constant]]
        if len(places) != 1:
            return None
        children[places[0]] = ("element", constants[constant[1]])
        return constants[constant[1]], dict(wrapper[1], children=children)
    return None


def component_renderings(source: str, name: str) -> list[dict]:
    """Every JSX tree the top-level ``function NAME`` of a ``.tsx`` source renders, the plain one first.

    ``return <...>;`` is the one rendering (as ``component_return_jsx``). The other shape read is a constant tree
    shown either as it is or inside one wrapper element (``RecoveryStatus`` closed under ``<details>``)::

        const X = <...>;
        return CONDITION ? <wrapper>...{X}...</wrapper> : X;        (or ``? X : <wrapper>...``)

    which gives ``[X, wrapper]``; the wrapper's ``{X}`` child is X's own tree, so a region found in both
    renderings is one object. A ``return`` of anything else (``return null``) renders nothing and is skipped, as
    in ``component_return_jsx``. Both shapes together, more than one of either, or neither is ``Unsupported``.
    """
    found, others, constants = _component_top_level(source, name)
    wrapped = [pair for pair in (_wrapped_constant(run, constants) for run in others) if pair is not None]
    if not wrapped:
        if len(found) != 1:
            raise Unsupported(f"component {name} returns JSX {len(found)} times at its top level")
        return found
    if found or len(wrapped) != 1:
        raise Unsupported(f"component {name} returns JSX {len(found)} times and a wrapped constant "
                          f"{len(wrapped)} times at its top level")
    return list(wrapped[0])


def find_elements(tree: dict, attribute: str, value: str) -> list[dict]:
    """Every element (also inside ``{expression}`` children and conditionals) with ``attribute="value"``."""
    found = [tree] if tree["attrs"].get(attribute) == ("str", value) else []
    for kind, child in tree["children"]:
        if kind == "element":
            found += find_elements(child, attribute, value)
        elif kind == "expr":
            for token_kind, token in child:
                if token_kind == "jsx":
                    found += find_elements(token, attribute, value)
    return found


def render_region(region: dict, variables: dict, module: "Module") -> list[str]:
    """The lines one region of a component shows for ``variables`` (its state and ``t``), in source order."""
    return jsx_lines(render_element(region, [dict(variables)], module))
