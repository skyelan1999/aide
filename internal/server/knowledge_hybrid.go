package server

import (
	"fmt"
	"math"
	"sort"
)

// Rank indices bind both retrieval channels to the same extracted source
// snapshot; a provider never supplies file identity, locators or citation text.
type documentRank struct {
	Index int
	Score float64
}

func documentKeywordRanks(chunks []documentHit, text string) []documentRank {
	query := documentTokens(text)
	df := map[string]int{}
	terms := make([]map[string]float64, len(chunks))
	for i, c := range chunks {
		terms[i] = documentTokens(c.Text)
		for term := range terms[i] {
			df[term]++
		}
	}
	idf := func(t string) float64 { return math.Log(1+float64(len(chunks))/float64(1+df[t])) + 1 }
	qn := 0.0
	for t, v := range query {
		qn += math.Pow(v*idf(t), 2)
	}
	ranks := []documentRank{}
	for i := range chunks {
		dot, norm := 0.0, 0.0
		for t, v := range terms[i] {
			w := (1 + math.Log(v)) * idf(t)
			norm += w * w
			dot += w * query[t] * idf(t)
		}
		if dot > 0 && qn > 0 && norm > 0 {
			ranks = append(ranks, documentRank{i, dot / math.Sqrt(qn*norm)})
		}
	}
	sort.SliceStable(ranks, func(i, j int) bool { return ranks[i].Score > ranks[j].Score })
	return ranks
}

// Normalize with Hypot to avoid overflow/underflow of sum-of-squares. A zero
// document vector has no semantic evidence and does not enter the ranked list.
func documentUnitVector(v []float64, dimension int) ([]float64, error) {
	if len(v) != dimension || dimension < 1 || dimension > 4096 {
		return nil, fmt.Errorf("embedding dimension mismatch or exceeds limit")
	}
	norm := 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, fmt.Errorf("embedding contains non-finite value")
		}
		norm = math.Hypot(norm, x)
	}
	if math.IsInf(norm, 0) {
		return nil, fmt.Errorf("embedding norm overflow")
	}
	out := make([]float64, dimension)
	if norm > 0 {
		for i, x := range v {
			out[i] = x / norm
		}
	}
	return out, nil
}
func documentVectorRanks(query []float64, vectors [][]float64, count int) ([]documentRank, error) {
	if count < 0 || count > 2500 || len(vectors) != count {
		return nil, fmt.Errorf("embedding count mismatch or exceeds limit")
	}
	q, err := documentUnitVector(query, len(query))
	if err != nil {
		return nil, err
	}
	ranks := []documentRank{}
	for i, v := range vectors {
		u, err := documentUnitVector(v, len(q))
		if err != nil {
			return nil, err
		}
		dot := 0.0
		for j, x := range u {
			dot += x * q[j]
		}
		if dot > 0 {
			ranks = append(ranks, documentRank{i, math.Min(1, dot)})
		}
	}
	sort.SliceStable(ranks, func(i, j int) bool { return ranks[i].Score > ranks[j].Score })
	return ranks, nil
}

// Weighted reciprocal rank fusion avoids comparing TF-IDF and cosine score
// magnitudes. Every candidate retains its original provenance and digest.
func documentFuseRanks(chunks []documentHit, keyword, vector []documentRank, vectorWeight float64, limit int) ([]documentHit, error) {
	if math.IsNaN(vectorWeight) || math.IsInf(vectorWeight, 0) || vectorWeight < 0 || vectorWeight > 1 || limit < 1 || limit > 40 {
		return nil, fmt.Errorf("invalid hybrid retrieval weights or limit")
	}
	scores := map[int]float64{}
	for channel, ranks := range [][]documentRank{keyword, vector} {
		weight := 1 - vectorWeight
		if channel == 1 {
			weight = vectorWeight
		}
		seen := map[int]bool{}
		for rank, item := range ranks {
			if item.Index < 0 || item.Index >= len(chunks) || math.IsNaN(item.Score) || math.IsInf(item.Score, 0) || item.Score <= 0 || seen[item.Index] {
				return nil, fmt.Errorf("invalid or duplicate retrieval rank")
			}
			seen[item.Index] = true
			if weight > 0 {
				scores[item.Index] += weight / float64(60+rank+1)
			}
		}
	}
	indices := make([]int, 0, len(scores))
	for i := range scores {
		indices = append(indices, i)
	}
	sort.Slice(indices, func(i, j int) bool {
		if scores[indices[i]] != scores[indices[j]] {
			return scores[indices[i]] > scores[indices[j]]
		}
		return indices[i] < indices[j]
	})
	if len(indices) > limit {
		indices = indices[:limit]
	}
	hits := make([]documentHit, 0, len(indices))
	for _, i := range indices {
		hit := chunks[i]
		hit.Score = scores[i]
		hits = append(hits, hit)
	}
	return hits, nil
}
