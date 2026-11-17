# What is measured

Each measurement is an item with a whole-number value, in the `production` or the `tests` scope. Files that match `files.tests` are tests.

| Metric | Scope | Item and value |
| --- | --- | --- |
| `function_lines` | both | The lines of a function that hold code. Comment and blank lines do not count. Lines of nested functions do. |
| `complexity` | both | Go: gocognit's cognitive complexity, closures included. TypeScript: 1 plus 1 for each `if`, loop, `catch`, `case`, `?:`, `&&`, `\|\|` and `??` in the function itself, not in nested functions. |
| `file_lines` | both | The lines in a source file. |
| `fan_out` | production only | The local packages (Go folders) or local modules (TypeScript files) that a package or module imports. |
| `duplicate_lines` | both | The lines in a file that belong to a repeated stretch of at least 100 tokens. Every file is compared with every other file. |
| `duplicate_groups` | both | Each group of repeated stretches is one item. Its value is the number of copies. A group is in `production` if any copy is in `production`. |
| `cycles` | production only | Each import cycle is one item, named after its members. |

## Function names

- Go measures declared functions and methods, named `path:Receiver.Name`.
- TypeScript measures every function with a body, named after the surrounding functions, such as `mount/render`.
- A callback takes the name of the call it goes to, then that call's first string argument, such as `test("sends")`.
- `#2`, `#3` and later numbers tell apart functions with the same name.

## JSX

JSX counts like other code. A line with a JSX element is a code line. A JSX element adds nothing to complexity. `&&`, `||`, `??` and `?:` inside JSX add 1 each.

## Duplicates

- Go uses dupl's syntax clones.
- TypeScript finds identical runs of 100 tokens and ignores spaces and comments.
- The name of a duplicate group identifies the files it spans and has a hash of the repeated code.
