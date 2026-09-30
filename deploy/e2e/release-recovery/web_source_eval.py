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
"""
from __future__ import annotations

import datetime as dt
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
REGEX_PRECEDERS = {"(", ",", "=", ":", "[", "!", "&&", "||", "??", "?", "{", "}", ";", "return", "===", "!==",
                   "==", "!=", "+", "-", "typeof"}


# -- tokenizer --------------------------------------------------------------------

def tokenize(source: str) -> list[tuple[str, Any]]:
    tokens: list[tuple[str, Any]] = []
    i, n = 0, len(source)
    while i < n:
        c = source[i]
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
        if c == "/" and (not tokens or tokens[-1][1] in REGEX_PRECEDERS and tokens[-1][0] in ("punct", "kw")):
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
    tokens.append(("eof", None))
    return tokens


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
                body = close + 1
                while tokens[body][0] != "eof" and tokens[body] != ("punct", "{"):
                    body += 1
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
    raise Unsupported(f"expression {kind}")
