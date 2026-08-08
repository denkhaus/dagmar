# dagmar-review skill

Spawn the dagmar-review subagent to review changes and write a report to docs/review/.

## When to use

After a major work session (multiple ADRs, code changes, docs updates) — invoke to get an
independent standards + spec + coherence review.

## How to use

From IPython:

```python
import yaml

with open(".agents/agents/dagmar-review.yaml") as f:
    spec = yaml.safe_load(f)

# Determine review range (last review shorthash..HEAD by default)
import subprocess
sha = subprocess.check_output(["git", "rev-parse", "--short", "HEAD"], text=True).strip()

# Append task context to the prompt
prompt = spec["prompt"] + f"\n\n## TASK\n\nReview everything since the last review report. Current HEAD: {sha}"

handle = await rlm(prompt, name="dagmar-review")
```

The agent writes the report to `docs/review/NN-YYYY-MM-DD-<sha>-session.md` and replies via
`agent_message.send(message, receiver_role='parent')`.

## What the agent does

1. Determines scope (commits since last review)
2. Reviews along axes A–F: Standards, Spec, Inconsistencies, Undefined terms, Missing ADRs, Housekeeping
3. Tags findings: [FIX], [GAP], [HOUSE], [SPEC]
4. Writes numbered report
5. Replies in German (<200 words) with top findings

## What the agent does NOT do

- No seeds filing (`sd create/update/close`)
- No fixes
- No planning hand-offs
