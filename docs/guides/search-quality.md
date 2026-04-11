# Enhanced Search & Query Quality

sage-wiki v0.1.3 introduces an enhanced search pipeline with chunk-level indexing, LLM query expansion, and LLM re-ranking. These features significantly improve retrieval quality for natural language Q&A queries.

## How it works

The enhanced pipeline replaces the document-level search with a multi-stage process:

```
User Query
  -> Strong-signal probe (fast BM25, skip expansion if confident)
  -> Query expansion (LLM: keyword + semantic + hypothetical answer variants)
  -> Parallel search:
     +-- BM25 on original + keyword variants (chunk-level FTS5)
     +-- Vector search on semantic/hyde variants (chunk-level, BM25-prefiltered)
  -> RRF fusion (reciprocal rank fusion)
  -> Deduplicate to document level (best chunk per doc)
  -> LLM re-ranking (top-15 candidates scored for relevance)
  -> Position-aware blending (retrieval + rerank scores)
  -> Read full articles + ontology traversal
  -> LLM synthesis
```

The original document-level pipeline remains as a fallback when chunk data is unavailable.

## Key features

### Chunk-level indexing

Articles are split into ~800-token chunks during compilation. Each chunk gets its own FTS5 entry and vector embedding. This means a search for "flash attention" can find the relevant paragraph inside a 3000-token article about Transformer Architecture, instead of relying on the whole-document embedding.

Chunks are indexed automatically during `sage-wiki compile`. On first compile after upgrading, existing articles are backfilled without requiring a full recompile.

### Query expansion

A single LLM call generates three types of search variants:

- **lex** (2 variants) — keyword-rich rewrites for BM25 (e.g., "flash attention" -> "flash attention GPU memory optimization", "attention SRAM tiling")
- **vec** (1 variant) — natural language rewrite for vector search
- **hyde** (1 variant) — hypothetical answer sentence for embedding similarity

A **strong-signal check** runs first: if the top BM25 result is already highly confident (normalized score >= 0.4 with 2x gap over #2), expansion is skipped entirely. This saves an LLM call on simple queries.

### LLM re-ranking

After retrieval, the top 15 candidates are sent to the LLM in a single call for relevance scoring. Each chunk is truncated to 400 tokens, with a total budget of 8000 tokens.

**Position-aware blending** protects high-confidence retrieval results from reranker noise:

| Retrieval rank | Retrieval weight | Reranker weight |
|---|---|---|
| 1-3 | 75% | 25% |
| 4-10 | 60% | 40% |
| 11+ | 40% | 60% |

This ensures that if RRF placed something at rank 1 with high confidence, the reranker can't easily demote it.

### BM25-prefiltered vector search

Instead of brute-force scanning all chunk vectors, the enhanced pipeline uses BM25 results as a candidate filter. Vector comparisons are limited to chunks from the top 50 BM25 documents, capping cosine computations at ~250 regardless of wiki size.

## Configuration

All features are enabled by default with zero config. Add these to `config.yaml` to customize:

```yaml
search:
  hybrid_weight_bm25: 0.7    # BM25 vs vector weight (doc-level fallback)
  hybrid_weight_vector: 0.3
  default_limit: 10
  query_expansion: true       # LLM query expansion (default: true)
  rerank: true                # LLM re-ranking (default: true)
  chunk_size: 800             # tokens per chunk for indexing (100-5000, default: 800)
```

### Disabling features

```yaml
# Disable expansion (saves ~1 LLM call per query)
search:
  query_expansion: false

# Disable re-ranking (saves ~1 LLM call per query)
search:
  rerank: false

# Disable both (chunk-level BM25+vector search still active)
search:
  query_expansion: false
  rerank: false
```

### Local models (Ollama)

When using Ollama as the LLM provider, re-ranking is automatically disabled by default. Local models often struggle with the structured JSON output that reranking requires. To force-enable it:

```yaml
api:
  provider: ollama
search:
  rerank: true    # explicitly enable for capable local models
```

Query expansion works well with most local models and remains enabled.

### Chunk size tuning

The default chunk size of 800 tokens works well for most content. Adjust if:

- **Shorter chunks (400-600):** Technical docs with dense, self-contained paragraphs
- **Longer chunks (1000-1500):** Narrative content where context spans multiple paragraphs
- **Maximum (5000):** Effectively disables chunking (one chunk per article)

```yaml
search:
  chunk_size: 600   # smaller chunks for technical docs
```

## Cost

**With local models (Ollama): free.** Chunk-level indexing and query expansion run locally at no cost. Re-ranking is auto-disabled for local models (see above), so the enhanced pipeline adds zero API cost. You still get chunk-level BM25+vector search and LLM query expansion — just no re-ranking.

**With cloud LLMs:** the enhanced pipeline adds two small LLM calls per Q&A query:

| Component | Tokens | Cost (Gemini Flash) |
|---|---|---|
| Query expansion | ~100 in, ~80 out | ~$0.0001 |
| Re-ranking | ~2000 in, ~200 out | ~$0.0005 |
| Extra embeddings | 3-4 vectors | ~$0.00003 |
| **Total per query** | | **~$0.0006** |

For context, that's less than $1 for 1,500 queries. The strong-signal optimization skips expansion entirely for simple keyword queries, further reducing cost. Both expansion and re-ranking can be disabled via config if needed.

## Comparison with qmd

sage-wiki's enhanced search pipeline was inspired by analyzing [qmd](https://github.com/dmayboroda/qmd)'s retrieval approach. Here's how they compare:

| Feature | sage-wiki | qmd |
|---|---|---|
| **Chunk indexing** | FTS5 + vector per chunk | Vector-only chunks |
| **Chunk size** | 800 tokens (configurable) | 900 tokens |
| **Query expansion** | LLM-based (lex/vec/hyde) | LLM-based |
| **Re-ranking** | LLM batch scoring + position-aware blending | Cross-encoder |
| **Vector search** | BM25-prefiltered (caps at ~250 comparisons) | Brute-force |
| **Hybrid search** | RRF fusion (BM25 + vector) | Vector-only |
| **Strong-signal skip** | Yes (normalized BM25 threshold) | No |
| **Ontology context** | 1-hop graph traversal adds related articles | No graph |
| **Model dependency** | Any provider (cloud or local via Ollama) | Local GGUF models |
| **Cost per query** | Free (Ollama) / ~$0.0006 (cloud) | Free (local) |

Key differences:

- **sage-wiki uses dual-channel retrieval** (BM25 + vector) at both document and chunk level, while qmd relies primarily on vector similarity. BM25 excels at exact keyword matches that vector search misses.
- **sage-wiki's position-aware blending** protects high-confidence retrieval results from reranker noise, using different weight tiers based on pre-rerank position.
- **sage-wiki adds ontology context** — after search, related concepts are discovered via graph traversal and added to the LLM synthesis context.
- **Both support local models for free inference.** qmd uses GGUF via llama.cpp; sage-wiki supports Ollama (or any OpenAI-compatible local server). With Ollama, sage-wiki's enhanced search is completely free — chunk indexing, query expansion, and BM25+vector search all run locally. Re-ranking is auto-disabled for local models but can be force-enabled for capable ones. With cloud LLMs, the additional cost per query is negligible (~$0.0006).

## Fallback behavior

The enhanced pipeline degrades gracefully:

- **No chunks indexed yet** — Falls back to document-level search. Logs: "chunk index empty — using document-level search."
- **LLM expansion fails** — Uses the raw query without variants.
- **LLM reranking fails** — Uses RRF order as-is.
- **No embedder configured** — BM25-only search with expansion keywords.
- **Empty wiki** — Returns "no results" immediately.

## Migration

Upgrading to v0.1.3 adds chunk tables automatically (`migrationV3`). No manual steps needed. On the first `sage-wiki compile` after upgrading, existing articles are chunk-indexed via backfill — this runs once and is transparent.
