# LLM Provider Research for Dagmar

**Research Date:** 2026-07-30
**Ticket:** wayfinder #7
**Branch:** `research/llm-provider`

## Summary

This research evaluates LLM providers for **dagmar** — a Dagger/Kubernetes-hybrid multi-agent autonomous coding system written in Go. The primary use case is **autonomous coding**, which demands strong code generation/refactoring capabilities, large context windows (for full codebase analysis), robust tool/function calling, and reliable self-hosting options.

**Key Findings:**
- **Anthropic Claude** leads for autonomous coding with 1M token context, excellent tool use, and strong coding benchmarks (SWE-bench, Terminal-Bench)
- **OpenAI** provides competitive models but documentation is less transparent about specific capabilities
- **Google Gemini** offers 1M token context and competitive pricing but lags in Go SDK maturity
- **Local models (Ollama)** provide self-hosting at compute cost, with trade-offs in capability

**Recommendation:** Start with **Claude Sonnet 5** as the default provider, with a provider-agnostic Go interface that enables swapping to Opus 4.8, Fable 5, or local models as needed.

---

## Provider Comparison Table

| Feature | Anthropic Claude | OpenAI | Google Gemini | Ollama (Local) |
|---------|------------------|--------|---------------|----------------|
| **Top Models for Coding** | Fable 5, Opus 4.8, Sonnet 5 | GPT-5.6 Sol, GPT-5.6 Terra, GPT-5.6 Luna | Gemini 2.5 Flash | Llama 3.1, Qwen 2.5, DeepSeek |
| **Context Window** | 1M tokens (Fable/Opus/Sonnet 5)<br>200k tokens (Haiku 4.5) | 54k (GPT Instant)<br>256k (GPT Reasoning) | 1M tokens (Gemini 2.5 Flash) | Varies by model (typically 32k-128k) |
| **Max Output** | 128k tokens | ~128k tokens | Varies | Varies by model |
| **Tool/Function Calling** | Excellent (native tool use, MCP integration, parallel tools) | Yes (parallel function calling on GPT-5+) | Yes (function calling) | Limited (depends on model) |
| **Coding Benchmarks** | Sonnet 5: 85.2% SWE-bench Verified<br>Opus 4.8: Highest accuracy | Competitive (specific numbers not published) | Competitive (specific numbers not published) | Lower (varies by model) |
| **Pricing (per MTok)** | Fable 5: $10/$50<br>Opus 4.8: $5/$25<br>Sonnet 5: $3/$15 (intro: $2/$10)<br>Haiku 4.5: $1/$5 | Not publicly listed (requires account) | Gemini 2.5 Flash: Very competitive | Free (compute cost only) |
| **Rate Limits** | Tier-based (Start/Build/Scale)<br>Custom for enterprise | Not publicly documented | Tier-based | N/A (local) |
| **Official Go SDK** | `github.com/anthropics/anthropic-sdk-go` | `github.com/openai/openai-go/v3` | `github.com/google/generative-ai-go` | `github.com/ollama/ollama/api` |
| **Self-Host Viability** | No (cloud-only) | No (cloud-only) | No (cloud-only) | **Yes** (primary strength) |
| **Knowledge Cutoff** | Fable 5: Jan 2026<br>Opus 4.8: May 2026<br>Sonnet 5: Jan 2026<br>Haiku 4.5: Feb 2025 | Not specified | Not specified | Varies by model |

### Claude Model Details (as of July 2026)

| Model ID | Description | Context | Max Output | Pricing (in/out) | Best For |
|----------|-------------|---------|------------|------------------|----------|
| `claude-fable-5` | Next-gen for long-running agents | 1M tokens | 128k tokens | $10/$50 MTok | Hardest autonomous work |
| `claude-opus-5`¹ | Complex agentic coding (latest: Opus 4.8) | 1M tokens | 128k tokens | $5/$25 MTok | Accuracy-critical work |
| `claude-sonnet-5` | Speed + intelligence balance | 1M tokens | 128k tokens | $3/$15 MTok (intro: $2/$10) | Default coding/research |
| `claude-haiku-4-5` | Fastest, near-frontier | 200k tokens | 64k tokens | $1/$5 MTok | Quick tasks, filtering |

¹ Note: Claude Opus 5 does not exist as of July 2026. The latest Opus-class model is Opus 4.8. Fable 5 is the new flagship above Opus.

---

## Recommended Go Abstraction

Given dagmar's Go codebase and need for provider flexibility, we recommend a **provider-agnostic interface** with the following structure:

### Core Interface Design

```go
package llm

// Provider is the interface for LLM providers
type Provider interface {
    // Complete generates a response for a single prompt
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

    // Chat completes a multi-turn conversation
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

    // StreamComplete streams a completion response
    StreamComplete(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
}

// CompletionRequest parameters
type CompletionRequest struct {
    Model    string            // Model identifier
    Prompt   string            // The prompt text
    MaxTokens int             // Maximum tokens to generate
    Temperature float64       // Sampling temperature
    Tools    []Tool           // Available tools for function calling
    Context  []Message        // Conversation history
    Metadata map[string]string // Provider-specific metadata
}

// Tool represents a function the LLM can call
type Tool struct {
    Name        string                 // Tool identifier
    Description string                 // What the tool does
    Parameters  map[string]interface{} // JSON schema for parameters
    Handler     ToolHandler           // Function to execute
}

// Message represents a single message in a conversation
type Message struct {
    Role    string // "user", "assistant", "system", "tool"
    Content string // Message content
    ToolID  string // For tool response messages
}
```

### Provider Implementations

1. **Claude Provider** (`llm/claude`)
   - Uses `github.com/anthropics/anthropic-sdk-go`
   - Maps Anthropic-specific tool use format
   - Handles prompt caching and extended thinking

2. **OpenAI Provider** (`llm/openai`)
   - Uses `github.com/openai/openai-go/v3`
   - Maps OpenAI function calling format
   - Handles structured outputs

3. **Gemini Provider** (`llm/gemini`)
   - Uses `github.com/google/generative-ai-go`
   - Maps Google's function calling format

4. **Ollama Provider** (`llm/ollama`)
   - Uses `github.com/ollama/ollama/api`
   - Enables local model execution
   - No API keys required

### Configuration

```go
// Config holds provider configuration
type Config struct {
    Provider  string                 // "claude", "openai", "gemini", "ollama"
    APIKey    string                 // API key (not for Ollama)
    BaseURL   string                 // Optional override for API endpoint
    Model     string                 // Default model
    Defaults  map[string]interface{} // Provider-specific defaults
}

// NewProvider creates a provider from configuration
func NewProvider(cfg *Config) (Provider, error)
```

### Routing Strategy

For autonomous coding, implement a **routing layer** that selects providers/models based on task complexity:

```go
type Router struct {
    providers map[string]Provider
    strategy  RoutingStrategy
}

type RoutingStrategy func(task *Task) string

// Example routing:
func RouteByComplexity(task *Task) string {
    switch task.Complexity {
    case "trivial":
        return "ollama:llama3.2"      // Local, free
    case "standard":
        return "claude:sonnet-5"      // Balanced
    case "complex":
        return "claude:opus-4.8"      // High accuracy
    case "critical":
        return "claude:fable-5"       // Maximum capability
    }
}
```

---

## Default Provider Recommendation

**Recommended Default: Claude Sonnet 5**

### Rationale

1. **Coding Excellence**: 85.2% on SWE-bench Verified, strong across autonomous coding benchmarks
2. **Context Window**: 1M tokens enables full-repo analysis without chunking
3. **Tool Use**: Native tool calling with MCP integration aligns with Dagger's container-based tools
4. **Cost-Effective**: $3/$15 per MTok (intro $2/$10 through August 2026)
5. **Reliability**: Strong track record, transparent rate limits, excellent documentation
6. **Go SDK**: Official `anthropic-sdk-go` with first-class support

### Model Selection Strategy

| Task Type | Model | Reason |
|-----------|-------|--------|
| Default coding, research | Sonnet 5 | Best balance of speed, intelligence, cost |
| Accuracy-critical refactors | Opus 4.8 | Higher correctness, worth the cost |
| Multi-step autonomous workflows | Fable 5 | Long-running agent reliability |
| Quick filtering, pre-processing | Haiku 4.5 | Speed and cost efficiency |
| Privacy/self-contained | Ollama (Llama 3.1/Qwen) | No data egress, compute trade-off |

### When to Consider Alternatives

- **OpenAI**: If enterprise agreements already exist, or specific GPT features are needed
- **Gemini**: For cost-sensitive workloads where 1M context at lower pricing is critical
- **Ollama**: For air-gapped environments, privacy requirements, or cost optimization at scale

---

## Canopy + Sandbox Integration

### Canopy (os-eco) for Prompt Management

**Canopy** is the os-eco prompt management tool (`@os-eco/canopy-cli@0.2.6` is installed globally). It provides:

- **Git-native prompt composition** — prompts stored as versioned files alongside code
- **Inheritance and sections** — compose prompts from reusable components
- **Schema validation** — ensure prompt templates meet structure requirements
- **Pinning** — lock specific prompt versions for reproducibility

#### Integration Pattern

```go
// Canopy prompt loading
type CanopyClient struct {
    cliPath string
    repoDir string
}

func (c *CanopyClient) LoadPrompt(name string, version string) (string, error) {
    // Execute: canopy get --name=<name> --version=<version>
    // Returns rendered prompt template
}

// Usage in dagmar
prompt, _ := canopy.LoadPrompt("autonomous-coding", "v1.2.0")
req := &llm.ChatRequest{
    SystemPrompt: prompt,
    Messages:     conversation,
}
```

#### Dagger Integration for LLM Calls

Dagger's native **LLM primitive** enables hermetic execution of LLM calls within containerized environments:

```go
// Dagger LLM integration pattern
package dagmar

import (
    "dagger/my-module/internal/dagger"
)

func (m *DagmarModule) CodeAgent(repo *dagger.Directory) *dagger.Container {
    // Set up environment with codebase
    env := dag.Env().
        WithDirectoryInput("repo", repo, "codebase to analyze").
        WithContainerOutput("result", "modified codebase")

    // Execute LLM with tools in hermetic container
    work := dag.LLM().
        WithEnv(env).
        WithPrompt(canopy.LoadPrompt("refactor-agent")).
        WithTool("bash", dag.Container().From("alpine:latest")).
        WithTool("git", dag.Container().From("alpine:latest").WithExec(["apk", "add", "git"]))

    return work.Env().Output("result").AsContainer()
}
```

#### Key Integration Points

1. **Canopy supplies prompts**: Load versioned, composed prompts at runtime
2. **Dagger provides sandbox**: LLM and tools execute in isolated containers
3. **Hermetic execution**: API keys injected only into Dagger runtime, never leaked
4. **Reproducibility**: Prompt version + Dagger container digest = fully reproducible run

#### Benefits

- **Separation of concerns**: Prompts (Canopy), execution (Dagger), orchestration (dagmar)
- **Version control**: Prompts and execution environments both git-tracked
- **Zero trust**: API keys never touch developer machines or CI containers
- **Composability**: Canopy prompts inherit from templates; Dagger modules compose as Go functions

---

## Open Questions

1. **Rate Limit Handling**: How should dagmar handle provider rate limits during autonomous workflows? Implement backoff, queueing, or provider fallback?

2. **Cost Monitoring**: Should dagmar include token/cost tracking per agent run? Consider integrating with spend limits and budget controls.

3. **Prompt Caching**: Anthropic offers prompt caching discounts. Should the abstraction support caching hints, and how do we invalidate cached prompts?

4. **Streaming**: For long-running agents, is streaming responses critical for user feedback, or is batch response sufficient with progress indicators?

5. **Multi-Provider Fallback**: Should dagmar implement automatic fallback (e.g., Claude → Ollama) on rate limit or failure, or fail explicitly?

6. **Tool Definition Format**: How do we normalize tool definitions across providers? Anthropic uses input_schema, OpenAI uses strict JSON Schema, Gemini has its own format.

7. **Canopy Integration Depth**: Should dagmar call Canopy directly at runtime, or should prompts be baked in at build time? Trade-offs between dynamism and reproducibility.

---

## Sources

### Anthropic Claude
- [Models Overview](https://docs.anthropic.com/en/docs/about-claude/models)
- [Pricing](https://docs.anthropic.com/en/docs/about-claude/pricing)
- [Tool Use](https://docs.anthropic.com/en/docs/tool-use)
- [Go SDK](https://platform.claude.com/docs/en/cli-sdks-libraries/sdks/go)
- [anthropic-sdk-go GitHub](https://github.com/anthropics/anthropic-sdk-go)

### OpenAI
- [Models](https://platform.openai.com/docs/models)
- [Pricing](https://openai.com/api/pricing)
- [Function Calling](https://platform.openai.com/docs/guides/function-calling)
- [Go SDK](https://github.com/openai/openai-go)
- [Rate Limits](https://platform.openai.com/api/docs/guides/rate-limits)

### Google Gemini
- [Models](https://ai.google.dev/gemini-api/docs/models/gemini)
- [Pricing](https://ai.google.dev/pricing)
- [generative-ai-go GitHub](https://github.com/google/generative-ai-go)

### Ollama / Local Models
- [Ollama Library](https://ollama.com/library)
- [Ollama GitHub](https://github.com/ollama/ollama)
- [Ollama Go Client Guide](https://www.bytesizego.com/blog/exploring-ollama-running-llms-locally-with-go)
- [Ollama Go SDK](https://github.com/ollama/ollama/api)

### Canopy (os-eco)
- [os-eco Meta-project](https://github.com/jayminwest/os-eco)
- [canopy GitHub](https://github.com/jayminwest/canopy)

### Dagger
- [LLM Integration](https://docs.dagger.io/features/llm)
- [LLM Primitive Blog](https://dagger.io/blog/llm)
- [Dagger Cookbook](https://docs.dagger.io/cookbook/)

### Benchmarks & Analysis
- [Claude Opus 5 Guide](https://coursiv.io/blog/claude-opus-5)
- [Claude Code Model Selection](https://claudefa.st/blog/models/model-selection)
- [SWE-bench Results](https://livebench.ai) (via Reddit discussion)

---

**Document Version:** 1.0
**Next Review:** After initial dagmar architecture is finalized
