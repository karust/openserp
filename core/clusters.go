package core

import (
	"net/url"
	"slices"
	"sort"
	"strings"
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
		occurrences    []ClusterOccurrence
		scoreSum       float64
		bestRank       int
		enginesCount   int
		representative int
	}

	urls := make([]megaURLKey, len(results))
	for i, r := range results {
		urls[i] = megaURLKey{url: NormalizeURLForClustering(r.URL), ad: r.Type == ResultTypeAd}
	}
	// Apply a same-kind match to every occurrence of that exact URL.
	aliases := make(map[string]string)
	for i, key := range megaWebURLKeys(urls) {
		if key.url != urls[i].url {
			aliases[urls[i].url] = key.url
		}
	}
	byURL := make(map[string]*clusterAccum)
	urlOrder := []string{}

	for i, r := range results {
		key := urls[i].url
		if key == "" {
			continue
		}
		if alias := aliases[key]; alias != "" {
			key = alias
		}
		acc, exists := byURL[key]
		if !exists {
			acc = &clusterAccum{bestRank: r.Rank, representative: i}
			byURL[key] = acc
			urlOrder = append(urlOrder, key)
		}
		rank := r.Rank
		if rank <= 0 {
			rank = 1
		}
		acc.scoreSum += 1.0 / float64(rank)
		if !slices.ContainsFunc(acc.occurrences, func(occurrence ClusterOccurrence) bool {
			return occurrence.Engine == r.Engine
		}) {
			acc.enginesCount++
		}
		acc.occurrences = append(acc.occurrences, ClusterOccurrence{
			Engine:   r.Engine,
			Rank:     r.Rank,
			ResultID: r.ID,
		})
		if r.Rank > 0 && (acc.bestRank <= 0 || r.Rank < acc.bestRank) {
			acc.bestRank = r.Rank
		}

		current := acc.representative
		previous := results[current]
		betterRank := betterMegaRank(r.Rank, previous.Rank, r.Engine, previous.Engine)
		if preferHTTPS(urls[i].url, urls[current].url, betterRank) {
			acc.representative = i
		}
	}

	clusters := make([]Cluster, 0, len(byURL))
	for _, key := range urlOrder {
		acc := byURL[key]
		canonical := results[acc.representative]
		score := acc.scoreSum / float64(enginesQueried)
		if score > 1.0 {
			score = 1.0
		}
		clusters = append(clusters, Cluster{
			ID:           buildClusterID(key),
			CanonicalURL: canonical.URL,
			Domain:       canonical.Domain,
			Title:        canonical.Title,
			Occurrences:  acc.occurrences,
			EnginesCount: acc.enginesCount,
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

type megaURLKey struct {
	url string
	ad  bool
}

// Keep original keys when only one scheme appears, including explicit ports.
func megaWebURLKeys(urls []megaURLKey) []megaURLKey {
	if !slices.ContainsFunc(urls, func(key megaURLKey) bool {
		return strings.HasPrefix(key.url, "http://")
	}) {
		return urls
	}

	const (
		hasHTTP = 1 << iota
		hasHTTPS
	)
	keys := make([]megaURLKey, len(urls))
	schemes := make(map[megaURLKey]uint8)
	for i, original := range urls {
		u, err := url.Parse(original.url)
		if err != nil || u.User != nil || !isUsableResultURL(u) {
			continue
		}
		var scheme uint8
		port := u.Port()
		switch {
		case u.Scheme == "http" && (port == "" || port == "80"):
			scheme = hasHTTP
		case u.Scheme == "https" && (port == "" || port == "443"):
			scheme = hasHTTPS
		default:
			continue
		}
		u.Scheme = "https"
		if u.Path == "" {
			u.Path = "/"
		}
		if port != "" {
			u.Host = strings.TrimSuffix(u.Host, ":"+port)
		}
		key := megaURLKey{url: u.String(), ad: original.ad}
		keys[i] = key
		schemes[key] |= scheme
	}
	for i, key := range keys {
		if schemes[key] != hasHTTP|hasHTTPS {
			keys[i] = urls[i]
		}
	}
	return keys
}

func betterMegaRank(rank, previousRank int, engine, previousEngine string) bool {
	if rank == previousRank {
		return engine < previousEngine
	}
	return rank > 0 && (previousRank <= 0 || rank < previousRank)
}

func preferHTTPS(candidateURL, currentURL string, preferredByRank bool) bool {
	candidateHTTPS := strings.HasPrefix(candidateURL, "https://")
	currentHTTPS := strings.HasPrefix(currentURL, "https://")
	if candidateHTTPS != currentHTTPS {
		return candidateHTTPS
	}
	return preferredByRank
}

func buildClusterID(normalizedURL string) string {
	return "c_" + shortMD5(normalizedURL)
}

func roundScore(s float64) float64 {
	return float64(int(s*100+0.5)) / 100
}
