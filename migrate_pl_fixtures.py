#!/usr/bin/env python3
"""Mechanical Perl->Go fixture migration for internal/agent tests.

Follows the proven template of run_repair_test.go (the one FSM test already
migrated to Go fixtures):
  * perl math module literal      -> Go source (package main; Add returns a+b)
  * fixed perl math literal       -> Go source (Add returns 10)
  * perl test-program literal     -> standalone Go main with Add(2,3)!=10 check
  * path strings "math.pl"/"test.pl" and bare mentions -> math.go / test.go
Skips audit_sec3_probe_test.go entirely. Idempotent.
"""
import re, pathlib

MATH_GO = '''package main

func Add(a, b int) int {
\treturn a + b
}
'''
MATH_FIXED_GO = '''package main

func Add(a, b int) int {
\treturn 10
}
'''
TEST_MAIN_GO = '''package main

import "fmt"

func main() {
\tresult := Add(2, 3)

\tif result != 10 {
\t\tfmt.Printf("WRONG: %d\\n", result)
\t\tpanic("test failed")
\t}

\tfmt.Println("PASS")
}
'''

def is_math_broken(s):
    return 'sub add' in s and ('return $a + $b;' in s or 'return $a+$b;' in s)

def is_math_fixed(s):
    return 'sub add' in s and 'return 10;' in s

def is_test_prog(s):
    return ('require "./math' in s or 'my $result = add' in s) and 'PASS' in s

def sub_literal(m):
    q = m.group(1)
    body = m.group(2)
    if is_math_broken(body):
        return q + MATH_GO + q
    if is_math_fixed(body):
        return q + MATH_FIXED_GO + q
    if is_test_prog(body):
        return q + TEST_MAIN_GO + q
    return m.group(0)

LIT = re.compile(r'(")((?:[^"\\\n]|\\.)*)("|$)|(`)(?:(?!\4).)*?\4', re.S)

def process(p):
    t = p.read_text()
    orig = t
    # multi-line string literals (backtick or unterminated-looking doubles)
    def repl(m):
        q = m.group(1) or m.group(4)
        body = m.group(2) if m.group(1) else m.group(0)[1:-1]
        if is_math_broken(body):
            return q + MATH_GO + q
        if is_math_fixed(body):
            return q + MATH_FIXED_GO + q
        if is_test_prog(body):
            return q + TEST_MAIN_GO + q
        return m.group(0)
    t = re.sub(r'"((?:[^"\\]|\\)*)"|`[^`]*`', lambda m: (
        '"' + MATH_GO + '"' if m.group(1) and is_math_broken(m.group(1)) else
        '"' + MATH_FIXED_GO + '"' if m.group(1) and is_math_fixed(m.group(1)) else
        m.group(0)), t)
    t = re.sub(r'`[^`]*`', lambda m: (
        '`' + MATH_GO + '`' if is_math_broken(m.group(0)) else
        '`' + MATH_FIXED_GO + '`' if is_math_fixed(m.group(0)) else
        '`' + TEST_MAIN_GO + '`' if is_test_prog(m.group(0)) else
        m.group(0)), t, flags=re.S)
    # escaped-quoted single-line perl fixtures inside double literals
    def esc_sub(m):
        body = m.group(1)
        plain = body.replace('\\"','"').replace('\\n','\n').replace('\\t','\t')
        if is_math_broken(plain):
            return '"' + MATH_GO.replace('"','\\"').replace('\n','\\n').replace('\t','\\t') + '"'
        if is_math_fixed(plain):
            return '"' + MATH_FIXED_GO.replace('"','\\"').replace('\n','\\n').replace('\t','\\t') + '"'
        if is_test_prog(plain):
            return '"' + TEST_MAIN_GO.replace('"','\\"').replace('\n','\\n').replace('\t','\\t') + '"'
        return m.group(0)
    t = re.sub(r'"((?:[^"\\]|\\)*?)"', esc_sub, t)
    # path renames everywhere
    t = t.replace('math.pl', 'math.go').replace('test.pl', 'test.go')
    if t != orig:
        p.write_text(t)
        return True
    return False

changed = []
for f in sorted(pathlib.Path('internal/agent').glob('*_test.go')):
    if f.name == 'audit_sec3_probe_test.go':
        continue
    if f.name == 'run_repair_test.go':
        continue  # already migrated manually
    if process(f):
        changed.append(str(f))
print('\n'.join(changed))
print(f'changed={len(changed)}')
