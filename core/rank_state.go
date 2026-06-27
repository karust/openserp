package core

// RankState tracks organic, ad, and absolute ranks for mixed SERP rows.
type RankState struct {
	organicRank  int
	adRank       int
	absoluteRank int
}

// NewRankState seeds ranks for a 0-based page on a 10-results-per-page engine.
func NewRankState(pageNum int) *RankState {
	return NewRankStateAt(pageNum*10, pageNum*10+1)
}

// NewRankStateAt seeds ranks from explicit organic and absolute bases.
func NewRankStateAt(organicBase, absoluteBase int) *RankState {
	return &RankState{
		organicRank:  organicBase,
		adRank:       1,
		absoluteRank: absoluteBase,
	}
}

// Next reserves the rank pair for the next emitted row.
func (r *RankState) Next(isAd bool) (rank, absoluteRank int) {
	absoluteRank = r.absoluteRank
	r.absoluteRank++
	if isAd {
		rank = r.adRank
		r.adRank++
		return rank, absoluteRank
	}
	r.organicRank++
	return r.organicRank, absoluteRank
}

// SetSeparatedAdAbsoluteRanks gives separated ad/organic passes one mixed order.
func SetSeparatedAdAbsoluteRanks(results []SearchResult, start int) {
	adCount := 0
	for i := range results {
		if results[i].Ad {
			adCount++
			results[i].AbsoluteRank = start + results[i].Rank
		}
	}
	organicAbsoluteRank := start + adCount + 1
	for i := range results {
		if results[i].Ad {
			continue
		}
		results[i].AbsoluteRank = organicAbsoluteRank
		organicAbsoluteRank++
	}
}
