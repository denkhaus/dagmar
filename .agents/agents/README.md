# Agent Definitions

Repo-local agent definitions live as **continual-harness skills** (Python modules), not YAML
files. The skill module contains the agent prompt (single source of truth), spawn logic, and
task-context injection.

## Current agents

- **dagmar-review** — on-demand reviewer. Spawn via `await dagmar_review.run()` from IPython.
  The Python module is installed at the kernel venv's site-packages (`dagmar_review/`).

## Spawning from Prime Agent

```python
# Auto-detect range (last review..HEAD):
handle = await dagmar_review.run()

# With explicit range and scope:
handle = await dagmar_review.run(range="abc1234..HEAD", scope="adr-0022")
```
