package core

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// BuildClusters groups results by normalized URL and scores them by cross-engine
// agreement. enginesQueried is the total number of engines that were asked
// (denominator for the score formula).
//
// Score = sum(1/rank for each occurrence) / enginesQueried, capped at 1.0.
func BuildClusters(results []Result, enginesQueried int) []Cluster {
	if enginesQueried <= 0 {
		enginesQueried = 1
	}

	type clusterAccum struct {
		occurrences []ClusterOccurrence
		scoreSum    float64
		bestRank    int
		title       string
		canonicalURL string
		domain      string
	}

	// Group by result ID (which is derived from normalized URL + engine).
	// For clustering we group by normalized URL regardless of engine, so we
	// re-key on NormalizeURLForClustering(result.URL).
	byURL := map[string]*clusterAccum{}
	urlOrder := []string{}

	for _, r := range results {
		norm := NormalizeURLForClustering(r.URL)
		if norm == "" {
			continue
		}
		acc, exists := byURL[norm]
		if !exists {
			acc = &clusterAccum{
				bestRank:     r.Rank,
				title:        r.Title,
				canonicalURL: r.URL,
				domain:       r.Domain,
			}
			byURL[norm] = acc
			urlOrder = append(urlOrder, norm)
		}
		rank := r.Rank
		if rank <= 0 {
			rank = 1
		}
		acc.scoreSum += 1.0 / float64(rank)
		acc.occurrences = append(acc.occurrences, ClusterOccurrence{
			Engine:   r.Engine,
			Rank:     r.Rank,
			ResultID: r.ID,
		})
		if r.Rank > 0 && (acc.bestRank <= 0 || r.Rank < acc.bestRank) {
			acc.bestRank = r.Rank
			acc.title = r.Title
			acc.canonicalURL = r.URL
			acc.domain = r.Domain
		}
	}

	clusters := make([]Cluster, 0, len(byURL))
	for _, norm := range urlOrder {
		acc := byURL[norm]
		score := acc.scoreSum / float64(enginesQueried)
		if score > 1.0 {
			score = 1.0
		}
		clusters = append(clusters, Cluster{
			ID:           buildClusterID(norm),
			CanonicalURL: acc.canonicalURL,
			Domain:       acc.domain,
			Title:        acc.title,
			Occurrences:  acc.occurrences,
			EnginesCount: len(acc.occurrences),
			BestRank:     acc.bestRank,
			Score:        roundScore(score),
		})
	}

	// Sort by score descending, then best_rank ascending as tiebreak.
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Score != clusters[j].Score {
			return clusters[i].Score > clusters[j].Score
		}
		return clusters[i].BestRank < clusters[j].BestRank
	})
	return clusters
}

func buildClusterID(normalizedURL string) string {
	h := sha256.Sum256([]byte(normalizedURL))
	return "c_" + hex.EncodeToString(h[:12])
}

func roundScore(s float64) float64 {
	return float64(int(s*100+0.5)) / 100
}
