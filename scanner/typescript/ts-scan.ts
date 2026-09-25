// Measures the TypeScript files named on stdin (a JSON array), relative to the project
// folder given as the only argument, for the scanner: file and
// function lines, branch complexity, local imports, and lines and groups of identical
// runs of 100 tokens. A file with a syntax error fails the scan. Uses the compiler that
// the bun.lock next to this file pins, so the numbers do not drift.
import ts from "typescript";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";

const WINDOW = 100;
const K = ts.SyntaxKind;
const BRANCHES = new Set([
  K.IfStatement, K.ForStatement, K.ForInStatement, K.ForOfStatement, K.WhileStatement,
  K.DoStatement, K.CatchClause, K.CaseClause, K.ConditionalExpression,
]);
const LOGICAL = new Set([K.AmpersandAmpersandToken, K.BarBarToken, K.QuestionQuestionToken]);

type Fn = { name: string; lines: number; complexity: number };
type Token = { text: string; line: number };

// Bun starts outside the project, so the project's bunfig.toml and .env are not loaded.
process.chdir(process.argv[2]);
const paths: string[] = JSON.parse(await Bun.stdin.text());
const known = new Set(paths);
const syntaxErrors: string[] = [];

function physicalLines(raw: string): number {
  return raw.split("\n").length - (raw.endsWith("\n") ? 1 : 0);
}

function lineOf(s: ts.SourceFile, pos: number): number {
  return s.getLineAndCharacterOfPosition(pos).line + 1;
}

// A callback is named after the call it is passed to, plus the call's first string
// argument when it has one: test("sends"), addEventListener("click").
function calleeName(call: ts.CallExpression | ts.NewExpression, s: ts.SourceFile): string {
  const callee = call.expression;
  const name = ts.isPropertyAccessExpression(callee) ? callee.name.text : callee.getText(s);
  const first = call.arguments?.[0];
  return first && ts.isStringLiteralLike(first) ? `${name}(${JSON.stringify(first.text)})` : name;
}

function nameOf(n: ts.SignatureDeclaration, s: ts.SourceFile): string {
  if (ts.isConstructorDeclaration(n)) return "constructor";
  if (n.name) return n.name.getText(s);
  let p = n.parent;
  while (ts.isParenthesizedExpression(p)) p = p.parent;
  if (ts.isVariableDeclaration(p) || ts.isPropertyAssignment(p) || ts.isPropertyDeclaration(p)) {
    return p.name.getText(s);
  }
  if (ts.isBinaryExpression(p) && p.operatorToken.kind === K.EqualsToken) return p.left.getText(s);
  if (ts.isCallExpression(p) || ts.isNewExpression(p)) return calleeName(p, s);
  return "anonymous";
}

// Branches in the function itself; nested functions are measured on their own.
function complexity(fn: ts.Node): number {
  let n = 1;
  const count = (x: ts.Node) => {
    if (x !== fn && ts.isFunctionLike(x)) return;
    if (BRANCHES.has(x.kind)) n++;
    if (ts.isBinaryExpression(x) && LOGICAL.has(x.operatorToken.kind)) n++;
    ts.forEachChild(x, count);
  };
  count(fn);
  return n;
}

function resolveImport(file: string, spec: string): string | undefined {
  if (!spec.startsWith(".")) return undefined;
  const base = join(dirname(file), spec);
  return [base, `${base}.ts`, `${base}/index.ts`].find((c) => known.has(c));
}

function measure(file: string) {
  const raw = readFileSync(file, "utf8");
  const s = ts.createSourceFile(file, raw, ts.ScriptTarget.Latest, true);
  // The parser recovers from syntax errors; its diagnostics are the only sign of them.
  for (const d of (s as unknown as { parseDiagnostics: ts.Diagnostic[] }).parseDiagnostics) {
    const at = d.start === undefined ? "" : `:${lineOf(s, d.start)}`;
    syntaxErrors.push(`${file}${at}: ${ts.flattenDiagnosticMessageText(d.messageText, "\n")}`);
  }
  const functions: Fn[] = [];
  const imports = new Set<string>();
  const seen = new Map<string, number>();
  const visit = (n: ts.Node, parent: string) => {
    let scope = parent;
    if (ts.isFunctionLike(n) && "body" in n && n.body) {
      scope = parent ? `${parent}/${nameOf(n, s)}` : nameOf(n, s);
      const count = (seen.get(scope) ?? 0) + 1;
      seen.set(scope, count);
      if (count > 1) scope = `${scope}#${count}`;
      const lines = lineOf(s, n.end) - lineOf(s, n.getStart(s)) + 1;
      functions.push({ name: scope, lines, complexity: complexity(n) });
    }
    if (ts.isImportDeclaration(n) && ts.isStringLiteral(n.moduleSpecifier)) {
      const target = resolveImport(file, n.moduleSpecifier.text);
      if (target) imports.add(target);
    }
    ts.forEachChild(n, (c) => visit(c, scope));
  };
  visit(s, "");
  return { file, lines: physicalLines(raw), functions, imports: [...imports].sort(), tokens: tokens(raw, s) };
}

function tokens(raw: string, s: ts.SourceFile): Token[] {
  const out: Token[] = [];
  const scanner = ts.createScanner(ts.ScriptTarget.Latest, true, ts.LanguageVariant.Standard, raw);
  for (let k = scanner.scan(); k !== K.EndOfFileToken; k = scanner.scan()) {
    out.push({ text: scanner.getTokenText(), line: lineOf(s, scanner.getTokenStart()) });
  }
  return out;
}

// Lines of each file inside a run of WINDOW tokens that appears more than once, and the
// groups those runs form. Overlapping repeated windows in a file join one copy, and
// copies of the same windows join one group, named by a hash of the group's windows.
function duplicates(files: { file: string; tokens: Token[] }[]) {
  const windows = new Map<bigint, number[]>();
  const at: [string, number, number, bigint][] = [];
  for (const { file, tokens } of files) {
    for (let i = 0; i + WINDOW <= tokens.length; i++) {
      const key = BigInt(Bun.hash(tokens.slice(i, i + WINDOW).map((t) => t.text).join("\0")));
      windows.set(key, [...(windows.get(key) ?? []), at.length]);
      at.push([file, tokens[i].line, tokens[i + WINDOW - 1].line, key]);
    }
  }
  const parent = at.map((_, i) => i);
  const root = (i: number): number => (parent[i] === i ? i : (parent[i] = root(parent[i])));
  const repeated = (i: number) => windows.get(at[i][3])!.length > 1;
  for (const same of windows.values()) for (const i of same) parent[root(i)] = root(same[0]);
  for (let i = 1; i < at.length; i++) {
    if (at[i][0] === at[i - 1][0] && repeated(i) && repeated(i - 1)) parent[root(i)] = root(i - 1);
  }
  const covered = new Map<string, Set<number>>();
  const groups = new Map<number, { keys: Set<bigint>; files: string[]; copies: number }>();
  for (let i = 0; i < at.length; i++) {
    if (!repeated(i)) continue;
    const [file, first, last, key] = at[i];
    const lines = covered.get(file) ?? new Set<number>();
    for (let l = first; l <= last; l++) lines.add(l);
    covered.set(file, lines);
    const g = groups.get(root(i)) ?? { keys: new Set(), files: [], copies: 0 };
    g.keys.add(key);
    g.files.push(file);
    if (i === 0 || at[i - 1][0] !== file || !repeated(i - 1)) g.copies++;
    groups.set(root(i), g);
  }
  return {
    duplicates: Object.fromEntries([...covered].sort().map(([f, lines]) => [f, lines.size])),
    groups: [...groups.values()].map((g) => ({
      fingerprint: Bun.hash([...g.keys].sort().join(",")).toString(16).padStart(16, "0"),
      files: g.files,
      copies: g.copies,
    })),
  };
}

const measured = paths.map(measure);
if (syntaxErrors.length > 0) {
  process.stderr.write(`cannot measure files with syntax errors:\n${syntaxErrors.join("\n")}\n`);
  process.exit(1);
}
const result = {
  typescript: ts.version,
  files: measured.map(({ tokens, ...rest }) => rest),
  ...duplicates(measured),
};
process.stdout.write(JSON.stringify(result));
