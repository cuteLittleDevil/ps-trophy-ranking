package psn

// Score is Sony's published trophy point formula (bronze 15, silver 30, gold 90, platinum 300).
// Ranking uses this local value, not the trophyPoint field on trophySummary.
func Score(bronze, silver, gold, platinum int) int {
	return bronze*15 + silver*30 + gold*90 + platinum*300
}
