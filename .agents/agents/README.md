# Agent Definitions

Repo-local agent definitions for Prime Agent. Agents are implemented as **Python skills**
(source in `.agents/skills/<name>/`), installed into the Prime Agent kernel-venv for
importability.

## Current agents

- **dagmar-review** — on-demand reviewer. Source: `.agents/skills/dagmar-review/dagmar_review.py`.
  Spawn: `await dagmar_review.run()` from IPython. The module contains the full agent prompt
  (single source of truth), range auto-detection, and rlm() spawn logic.

## Install / Re-install

After a kernel restart or fresh checkout, install the skill module:

```bash
mkdir -p ~/.prime/agent/kernel-venv/lib/python3.11/site-packages/dagmar_review
cp .agents/skills/dagmar-review/dagmar_review.py \
   ~/.prime/agent/kernel-venv/lib/python3.11/site-packages/dagmar_review/__init__.py
```

Then verify: `python -c "import dagmar_review; print(dagmar_review.run)"`

## Spawning

```python
# Auto-detect range (last review..HEAD):
handle = await dagmar_review.run()

# With explicit range and scope:
handle = await dagmar_review.run(range="abc1234..HEAD", scope="adr-0022")
```
