package mempool

import "sort"

// Evict removes the lowest fee-rate transactions until the pool is at or below maxSize.
func (p *Pool) Evict() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.txs) <= p.maxSize {
		return 0
	}

	// Collect entries and sort by fee rate ascending (lowest first).
	entries := make([]*entry, 0, len(p.txs))
	for _, e := range p.txs {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].feeRate < entries[j].feeRate
	})

	evicted := 0
	for len(p.txs) > p.maxSize && evicted < len(entries) {
		p.removeLocked(entries[evicted].txHash)
		evicted++
	}
	return evicted
}
