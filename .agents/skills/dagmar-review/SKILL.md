# dagmar-review skill

Spawn the dagmar-review subagent to review changes and write a report to docs/review/.

## When to use

After a major work session — invoke to get an independent standards + spec + coherence
review. The reviewer advises only; does NOT execute (no seeds, no fixes).

## Usage

```python
# From the Prime Agent IPython kernel:
handle = await dagmar_review.run(_rlm=rlm)

# With explicit range and scope:
handle = await dagmar_review.run(range="abc1234..HEAD", scope="adr-0022", _rlm=rlm)
```

The `_rlm` parameter injects the Prime Agent spawn function (a kernel global)
into the skill module. This is required because Python modules cannot see
kernel globals directly.

## Install

The `dagmar_review.py` module in this directory is the source of truth. To make it
importable in the Prime Agent kernel, copy it to the kernel-venv site-packages:

```bash
mkdir -p ~/.prime/agent/kernel-venv/lib/python3.11/site-packages/dagmar_review
cp .agents/skills/dagmar-review/dagmar_review.py \
   ~/.prime/agent/kernel-venv/lib/python3.11/site-packages/dagmar_review/__init__.py
```
