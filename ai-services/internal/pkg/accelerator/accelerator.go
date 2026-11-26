// Package accelerator holds the vendor-neutral vocabulary used to reason about accelerator cards
// (IBM Spyre today, others such as AMD later): which kinds exist and how many of them a
// deployment needs or a worker has. Vendor specifics, like discovering the free cards of a kind,
// live next to the code that talks to the hardware (see the spyre subpackage).
package accelerator

import (
	"fmt"
	"maps"
	"slices"
)

// Type identifies a kind of accelerator card.
type Type string

const (
	// Spyre is the IBM Spyre accelerator.
	Spyre Type = "spyre"
)

// Counts is a number of cards per accelerator type. It is used both for what a deployment
// requires and for what a worker has available. A type that is absent counts as zero.
type Counts map[Type]int

// Add adds the counts of other to c. Zero counts are ignored. c must not be nil.
func (c Counts) Add(other Counts) {
	for t, n := range other {
		if n > 0 {
			c[t] += n
		}
	}
}

// IsEmpty reports whether no card of any type is counted.
func (c Counts) IsEmpty() bool {
	for _, n := range c {
		if n > 0 {
			return false
		}
	}

	return true
}

// Check verifies that available covers c, type by type. It returns an *InsufficientError for the
// first type, in name order, that falls short.
func (c Counts) Check(available Counts) error {
	for _, t := range slices.Sorted(maps.Keys(c)) {
		if need := c[t]; need > available[t] {
			return &InsufficientError{Type: t, Need: need, Have: available[t]}
		}
	}

	return nil
}

// InsufficientError is returned by Counts.Check when a worker has fewer cards of a type than needed.
type InsufficientError struct {
	Type Type
	Need int
	Have int
}

func (e *InsufficientError) Error() string {
	return fmt.Sprintf("insufficient %s accelerator cards: required %d, available %d", e.Type, e.Need, e.Have)
}
