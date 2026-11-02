package receipts

import "sort"

// percentile returns the p-th percentile (nearest rank) of v; 0 when empty.
func percentile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	rank := int(p/100*float64(len(s))+0.999999) - 1
	return s[max(0, min(rank, len(s)-1))]
}
