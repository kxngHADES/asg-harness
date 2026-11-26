"""List the third-party modules a Python script imports (used by ASG).

usage: infer_imports.py <entry.py> <root> [<root> ...]

Starts at the entry script and follows its local modules (files and
packages found in the roots), so helpers it imports are scanned too but
never mistaken for PyPI packages. Prints one JSON line:
{"required": [...], "optional": [...]}; "optional" are imports guarded by
try/except ImportError.
"""
import ast
import json
import os
import sys

SKIP = {"__pycache__", "venv", ".venv", "node_modules"}
STDLIB = set(sys.stdlib_module_names) | {"__future__", "__main__"}


def py_files(folder):
    out = []
    for dp, dn, fn in os.walk(folder):
        dn[:] = [d for d in dn if d not in SKIP and not d.startswith(".")]
        out += [os.path.join(dp, f) for f in fn if f.endswith(".py")]
    return out


def local_files(top, roots):
    """Files of a local module named top, or None if it isn't local."""
    for r in roots:
        f = os.path.join(r, top + ".py")
        if os.path.isfile(f):
            return [f]
        d = os.path.join(r, top)
        if os.path.isdir(d) and (os.path.isfile(os.path.join(d, "__init__.py")) or py_files(d)):
            return py_files(d)
    return None


def guarded(node, parents):
    """Is node inside a try that catches ImportError?"""
    while node in parents:
        node = parents[node]
        if isinstance(node, ast.Try):
            for h in node.handlers:
                names = []
                t = h.type
                if t is None:
                    return True
                for e in (t.elts if isinstance(t, ast.Tuple) else [t]):
                    if isinstance(e, ast.Name):
                        names.append(e.id)
                    elif isinstance(e, ast.Attribute):
                        names.append(e.attr)
                if {"ImportError", "ModuleNotFoundError", "Exception", "BaseException"} & set(names):
                    return True
    return False


def main():
    entry = os.path.abspath(sys.argv[1])
    roots = [os.path.abspath(r) for r in sys.argv[2:]] or [os.path.dirname(entry)]
    required, optional = set(), set()
    queue, seen = [entry], set()
    while queue:
        path = queue.pop()
        if path in seen:
            continue
        seen.add(path)
        try:
            with open(path, encoding="utf-8", errors="replace") as f:
                tree = ast.parse(f.read())
        except (OSError, SyntaxError, ValueError):
            continue
        # A module inside a local package may import its neighbours by bare
        # name (the package adds its folder to sys.path, or it is run from
        # there): resolve against the file's own folders too, up to a root.
        search = list(roots)
        d = os.path.dirname(path)
        while d not in search and d.startswith(tuple(roots)):
            search.append(d)
            parent_dir = os.path.dirname(d)
            if parent_dir == d:
                break
            d = parent_dir
        parents = {}
        for parent in ast.walk(tree):
            for child in ast.iter_child_nodes(parent):
                parents[child] = parent
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                mods = [a.name for a in node.names]
            elif isinstance(node, ast.ImportFrom):
                if node.level:  # relative import: a sibling module
                    if node.module:
                        sib = os.path.join(os.path.dirname(path), node.module.split(".")[0] + ".py")
                        if os.path.isfile(sib):
                            queue.append(sib)
                    continue
                mods = [node.module] if node.module else []
            else:
                continue
            for mod in mods:
                top = mod.split(".")[0]
                if not top or top in STDLIB:
                    continue
                files = local_files(top, search)
                if files is not None:
                    queue.extend(files)
                    continue
                (optional if guarded(node, parents) else required).add(mod)
    optional -= required
    print(json.dumps({"required": sorted(required), "optional": sorted(optional)}))


if __name__ == "__main__":
    main()
