package psn

// Score is the public trophy point formula: bronze 15, silver 30, gold 90, platinum 300.
func Score(bronze, silver, gold, platinum int) int {
	return bronze*15 + silver*30 + gold*90 + platinum*300
}
