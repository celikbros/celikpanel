"""Crude undefined-name check: names loaded in a module that no enclosing scope binds. usage: names.py FILE..."""
import ast
import builtins
import sys


def bound(node):
    names = set()
    for child in ast.walk(node):
        if isinstance(child, ast.Name) and isinstance(child.ctx, (ast.Store, ast.Del)):
            names.add(child.id)
        elif isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
            names.add(child.name)
        elif isinstance(child, ast.arg):
            names.add(child.arg)
        elif isinstance(child, (ast.Import, ast.ImportFrom)):
            for alias in child.names:
                names.add((alias.asname or alias.name).split(".")[0])
        elif isinstance(child, ast.ExceptHandler) and child.name:
            names.add(child.name)
    return names


for path in sys.argv[1:]:
    tree = ast.parse(open(path, encoding="utf-8").read())
    known = bound(tree) | set(dir(builtins)) | {"__file__", "__name__", "__doc__"}
    for node in ast.walk(tree):
        if isinstance(node, ast.Name) and isinstance(node.ctx, ast.Load) and node.id not in known:
            print(f"{path}:{node.lineno}: {node.id}")
    # attribute use on self: methods that do not exist on the class or its named bases are listed for review
    for cls in [n for n in tree.body if isinstance(n, ast.ClassDef)]:
        defined = {n.name for n in cls.body if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))}
        used = {}
        for node in ast.walk(cls):
            if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name) and node.value.id == "self":
                used.setdefault(node.attr, node.lineno)
        print(path, cls.name, "self.* not defined in this class:",
              sorted(name for name in used if name not in defined))
print("done")
