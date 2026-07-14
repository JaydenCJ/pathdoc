// Package shadow turns the scanned command index into shadowing
// conflicts: commands provided by more than one PATH directory. The
// first provider wins; each loser is classified as the same underlying
// file as the winner (benign — symlink farms, usr-merge) or a different
// file (the "wrong python" class of surprise).
package shadow

import "github.com/JaydenCJ/pathdoc/internal/scan"

// Provider is one candidate for a command name, in PATH order.
type Provider struct {
	scan.Binary
	Wins         bool // first in PATH order
	SameAsWinner bool // same underlying file as the winner
}

// Conflict is a command name with more than one provider.
type Conflict struct {
	Name      string
	Providers []Provider
	Benign    bool // every shadowed provider is the same file as the winner
}

// Distinct returns how many shadowed providers are different files.
func (c Conflict) Distinct() int {
	n := 0
	for _, p := range c.Providers {
		if !p.Wins && !p.SameAsWinner {
			n++
		}
	}
	return n
}

// Analyze extracts every conflict from the index, sorted by command
// name so output is deterministic.
func Analyze(idx *scan.Index) []Conflict {
	var out []Conflict
	for _, name := range idx.Names {
		bins := idx.ByName[name]
		if len(bins) < 2 {
			continue
		}
		c := Conflict{Name: name, Benign: true}
		winner := bins[0]
		for i, b := range bins {
			p := Provider{Binary: b, Wins: i == 0}
			if i > 0 {
				p.SameAsWinner = b.SameFile(winner)
				if !p.SameAsWinner {
					c.Benign = false
				}
			}
			c.Providers = append(c.Providers, p)
		}
		out = append(out, c)
	}
	return out
}

// OnlyDistinct filters out conflicts where every loser is the same file
// as the winner.
func OnlyDistinct(cs []Conflict) []Conflict {
	var out []Conflict
	for _, c := range cs {
		if !c.Benign {
			out = append(out, c)
		}
	}
	return out
}
