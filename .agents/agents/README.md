# Agent Definitions

Repo-local agent definitions for Prime Agent subagent spawning. Analogous to
`.omnigent/agent-configs/` but mapped to Prime Agent's `rlm()` runtime.

## Format

Each `.yaml` file in this directory defines one agent. The filename SHOULD match
the agent `name`.

```yaml
spec_version: 1
name: my-agent            # used as rlm(name=...)
description: >            # discoverability: what this agent does
  What it does and when to use it.
model: null               # optional: model selector (null = inherit parent)
spawn: false              # false = agent must not spawn its own sub-agents
working_dir: .            # cwd for the agent
prompt: |                 # the full system prompt / agent instructions
  You are ...
```

## Field reference

| Field | Required | Description |
|-------|----------|-------------|
| `spec_version` | yes | Format version (currently `1`). |
| `name` | yes | Agent identifier. Must be unique among siblings. Used as `rlm(name=name)`. Lowercase, hyphens. |
| `description` | yes | What the agent does. Human- and agent-readable. |
| `model` | no | Model selector string (e.g. `anthropic/claude-sonnet-4-20250514`). Omit or `null` to inherit the parent's model. |
| `spawn` | no | `false` forbids nested sub-agent spawning (the agent works alone). Default: `true`. |
| `working_dir` | no | Working directory. Default: `.` (repo root). |
| `prompt` | yes | The full system prompt — the agent's identity, instructions, and output contract. |

## Spawning from Prime Agent

```python
import yaml

with open(".agents/agents/dagmar-review.yaml") as f:
    spec = yaml.safe_load(f)

handle = await rlm(
    spec["prompt"],
    name=spec["name"],
    model=spec.get("model"),  # None = inherit
)
```

Pass task-specific context (git ranges, seed IDs) by appending to the prompt:

```python
prompt = spec["prompt"] + f"\n\nTASK: Review range {sha}..HEAD"
handle = await rlm(prompt, name=spec["name"])
```
