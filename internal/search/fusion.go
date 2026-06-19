package search

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/clcollins/mnemo/internal/embed"
	"github.com/clcollins/mnemo/internal/store"
)

const (
	rrfK              = 60
	ftsWeight         = 0.5
	importanceWeight  = 0.15
	recencyHalfLife   = 90.0
	recencyMaxBoost   = 0.05
	tokenBudgetRatio  = 0.6
	truncatedSuffix   = " [truncated]"
	truncatedMaxChars = 200
)

type Searcher interface {
	VectorSearch(ctx context.Context, queryVec []float32, f store.Filter, k int) ([]store.Ranked, error)
	KeywordSearch(ctx context.Context, terms string, f store.Filter, k int) ([]store.Ranked, error)
}

func HybridSearch(ctx context.Context, query string, embedder embed.Embedder, searcher Searcher, filter store.Filter, limit int, maxTokens int) ([]store.Ranked, error) {
	vecs, err := embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	vecResults, err := searcher.VectorSearch(ctx, vecs[0], filter, limit*3)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	keyTerms := extractKeyTerms(query)
	ftsQuery := buildFTSQuery(keyTerms)

	var ftsResults []store.Ranked
	if ftsQuery != "" {
		ftsResults, err = searcher.KeywordSearch(ctx, ftsQuery, filter, limit*3)
		if err != nil {
			return nil, fmt.Errorf("keyword search: %w", err)
		}
	}

	fused := fuseResults(vecResults, ftsResults)
	sort.Slice(fused, func(i, j int) bool {
		return fused[i].Score > fused[j].Score
	})

	if len(fused) > limit {
		fused = fused[:limit]
	}

	applyTokenBudget(fused, maxTokens)

	return fused, nil
}

func fuseResults(vecResults, ftsResults []store.Ranked) []store.Ranked {
	type scored struct {
		ranked store.Ranked
		score  float64
	}

	byID := make(map[string]*scored)

	for rank, r := range vecResults {
		id := r.Chunk.ID
		s, ok := byID[id]
		if !ok {
			s = &scored{ranked: r}
			byID[id] = s
		}
		s.score += 1.0 / float64(rrfK+rank+1)
		s.ranked.VecRank = rank + 1
	}

	for rank, r := range ftsResults {
		id := r.Chunk.ID
		s, ok := byID[id]
		if !ok {
			s = &scored{ranked: r}
			byID[id] = s
		}
		s.score += ftsWeight / float64(rrfK+rank+1)
		s.ranked.FTSRank = rank + 1
	}

	now := time.Now()
	var results []store.Ranked
	for _, s := range byID {
		s.score += importanceWeight * s.ranked.Chunk.Importance

		ageDays := now.Sub(s.ranked.Chunk.CreatedAt).Hours() / 24.0
		recencyBoost := recencyMaxBoost * (1.0 / (1.0 + ageDays/recencyHalfLife))
		s.score += recencyBoost

		s.ranked.Score = s.score
		results = append(results, s.ranked)
	}

	return results
}

func applyTokenBudget(results []store.Ranked, maxTokens int) {
	budget := int(float64(maxTokens) * tokenBudgetRatio)
	used := 0

	for i := range results {
		tokens := estimateTokens(results[i].Chunk.Content)
		if used+tokens <= budget {
			used += tokens
			continue
		}
		remaining := budget - used
		if remaining > 0 {
			results[i].Chunk.Content = truncateToChars(results[i].Chunk.Content, remaining*4) + truncatedSuffix
			used = budget
		} else {
			results[i].Chunk.Content = truncateToChars(results[i].Chunk.Content, truncatedMaxChars) + truncatedSuffix
		}
	}
}

func estimateTokens(text string) int {
	return int(math.Ceil(float64(len(text)) / 4.0))
}

func truncateToChars(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars]
}

var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "can": true, "shall": true,
	"to": true, "of": true, "in": true, "for": true, "on": true,
	"with": true, "at": true, "by": true, "from": true, "as": true,
	"into": true, "through": true, "during": true, "before": true,
	"after": true, "above": true, "below": true, "between": true,
	"and": true, "but": true, "or": true, "nor": true, "not": true,
	"so": true, "yet": true, "both": true, "either": true, "neither": true,
	"it": true, "its": true, "this": true, "that": true, "these": true,
	"those": true, "i": true, "you": true, "he": true, "she": true,
	"we": true, "they": true, "me": true, "him": true, "her": true,
	"us": true, "them": true, "my": true, "your": true, "his": true,
	"our": true, "their": true,
	"what": true, "which": true, "who": true, "whom": true, "where": true,
	"when": true, "why": true, "how": true,
}

func extractKeyTerms(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var terms []string
	for _, w := range words {
		cleaned := strings.Trim(w, "?!.,;:'\"()-")
		if cleaned == "" {
			continue
		}
		if stopwords[cleaned] {
			continue
		}
		terms = append(terms, cleaned)
	}
	return terms
}

func buildFTSQuery(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + t + `"`
	}
	return strings.Join(quoted, " ")
}
